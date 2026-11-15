package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// Options selects the rules a run applies.
type Options struct {
	// SlogPairs keeps each key and its value of a log/slog call on one line.
	SlogPairs bool
	// TrailingFuncLit keeps the arguments before a trailing function literal
	// on the call's first line, as in t.Run(name, func(t *testing.T) {.
	TrailingFuncLit bool
	// FixHeaders moves a multi-line if init before its if statement and a
	// struct literal out of an if condition into a variable.
	FixHeaders bool
}

// change replaces src[start:end] with text; start == end inserts. Changes of
// one group are applied together or not at all.
type change struct {
	start, end int
	text       string
	group      int
}

// Diagnostic is a place the rules are broken that the tool does not fix.
type Diagnostic struct {
	Position token.Position
	Message  string
}

func (diagnostic Diagnostic) String() string {
	return fmt.Sprintf(
		"%s:%d:%d: %s",
		diagnostic.Position.Filename,
		diagnostic.Position.Line,
		diagnostic.Position.Column,
		diagnostic.Message,
	)
}

// fileRules finds the changes one file needs. A pass applies them, formats the
// file, and the next pass looks again, until nothing changes.
type fileRules struct {
	fset        *token.FileSet
	src         []byte
	info        *types.Info
	pkg         *types.Package
	options     Options
	changes     []change
	diagnostics []Diagnostic
	groups      int
	// fixedIn holds the top-level functions that got a header fix this pass;
	// one per function per pass, so the next pass sees the variables it adds.
	fixedIn  map[ast.Node]bool
	function ast.Node
}

func (rules *fileRules) newGroup() int {
	rules.groups++
	return rules.groups
}

func (rules *fileRules) line(pos token.Pos) int {
	return rules.fset.Position(pos).Line
}

func (rules *fileRules) offset(pos token.Pos) int {
	return rules.fset.Position(pos).Offset
}

func (rules *fileRules) multiline(node ast.Node) bool {
	return rules.line(node.Pos()) != rules.line(node.End())
}

func (rules *fileRules) report(pos token.Pos, format string, arguments ...any) {
	rules.diagnostics = append(
		rules.diagnostics,
		Diagnostic{
			Position: rules.fset.Position(pos),
			Message:  fmt.Sprintf(format, arguments...),
		},
	)
}

func (rules *fileRules) insert(pos int, text string) {
	rules.changes = append(
		rules.changes,
		change{
			start: pos,
			end:   pos,
			text:  text,
			group: rules.newGroup(),
		},
	)
}

// canFixHeader reports whether the current function may get a header fix in
// this pass, and claims it.
func (rules *fileRules) canFixHeader(isElseIf bool) bool {
	if !rules.options.FixHeaders || isElseIf || rules.fixedIn[rules.function] {
		return false
	}
	rules.fixedIn[rules.function] = true
	return true
}

// breakBefore puts the node at pos on a new line when it shares a line with
// the end of the node before it.
func (rules *fileRules) breakBefore(previousEnd, pos token.Pos) {
	if rules.line(previousEnd) == rules.line(pos) {
		rules.insert(rules.offset(pos), "\n")
	}
}

// breakClose puts the closing token on its own line after the last item,
// adding the trailing comma gofmt then requires.
func (rules *fileRules) breakClose(lastEnd, closing token.Pos) {
	if rules.line(lastEnd) != rules.line(closing) {
		return
	}
	at := rules.offset(lastEnd)
	between := string(rules.src[at:rules.offset(closing)])
	trimmed := strings.TrimLeft(between, " \t")
	if strings.HasPrefix(trimmed, ",") {
		rules.insert(at+len(between)-len(trimmed)+1, "\n")
		return
	}
	rules.insert(at, ",\n")
}

// breakList puts each item between opening and closing on its own line.
func (rules *fileRules) breakList(opening token.Pos, items []ast.Node, closing token.Pos) {
	previous := opening
	for _, item := range items {
		rules.breakBefore(previous, item.Pos())
		previous = item.End()
	}
	rules.breakClose(previous, closing)
}

func (rules *fileRules) isStruct(literal *ast.CompositeLit) bool {
	typ := rules.info.TypeOf(literal)
	if typ == nil {
		return false
	}
	_, isStruct := typ.Underlying().(*types.Struct)
	return isStruct
}

// structLit returns the struct literal with fields that expression is, or
// takes the address of; the rules always split one.
func (rules *fileRules) structLit(expression ast.Expr) *ast.CompositeLit {
	if unary, ok := expression.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expression = unary.X
	}
	literal, ok := expression.(*ast.CompositeLit)
	if !ok || len(literal.Elts) == 0 || !rules.isStruct(literal) {
		return nil
	}
	return literal
}

// spansLines reports an expression that is or will be on several lines.
func (rules *fileRules) spansLines(expression ast.Expr) bool {
	return rules.multiline(expression) || rules.structLit(expression) != nil
}

// slogHead returns how many leading arguments of a log/slog call are not
// key-value attributes, or -1 when call is not such a call.
func (rules *fileRules) slogHead(call *ast.CallExpr) int {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return -1
	}
	fromSlog := false
	if ident, isIdent := selector.X.(*ast.Ident); isIdent {
		if name, isPkg := rules.info.Uses[ident].(*types.PkgName); isPkg && name.Imported().Path() == "log/slog" {
			fromSlog = true
		}
	}
	if typ := rules.info.TypeOf(selector.X); typ != nil {
		if pointer, isPointer := typ.(*types.Pointer); isPointer {
			typ = pointer.Elem()
		}
		if named, isNamed := typ.(*types.Named); isNamed && named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "log/slog" && named.Obj().Name() == "Logger" {
			fromSlog = true
		}
	}
	if !fromSlog {
		return -1
	}
	switch selector.Sel.Name {
	case "Debug", "Info", "Warn", "Error":
		return 1
	case "DebugContext", "InfoContext", "WarnContext", "ErrorContext":
		return 2
	case "Log":
		return 3
	case "With":
		return 0
	}
	return -1
}

func (rules *fileRules) call(call *ast.CallExpr) {
	if len(call.Args) == 0 {
		return
	}
	spans := rules.line(call.Lparen) != rules.line(call.Rparen)
	for _, argument := range call.Args {
		if rules.structLit(argument) != nil {
			spans = true
		}
	}
	if !spans {
		return
	}
	last := call.Args[len(call.Args)-1]
	if _, isFuncLit := last.(*ast.FuncLit); isFuncLit && rules.options.TrailingFuncLit &&
		rules.line(last.Pos()) == rules.line(call.Lparen) {
		fits := true
		for _, argument := range call.Args[:len(call.Args)-1] {
			if rules.spansLines(argument) {
				fits = false
			}
		}
		if fits {
			return
		}
	}
	head := len(call.Args)
	if rules.options.SlogPairs {
		if slogHead := rules.slogHead(call); slogHead >= 0 {
			head = slogHead
		}
	}
	previous := call.Lparen
	for index, argument := range call.Args {
		if index < head || (index-head)%2 == 0 {
			rules.breakBefore(previous, argument.Pos())
		}
		previous = argument.End()
	}
	if call.Ellipsis.IsValid() {
		previous = call.Ellipsis + token.Pos(len("..."))
	}
	rules.breakClose(previous, call.Rparen)
}

func (rules *fileRules) compositeLit(literal *ast.CompositeLit) {
	if len(literal.Elts) == 0 {
		return
	}
	items := make([]ast.Node, len(literal.Elts))
	for index, element := range literal.Elts {
		items[index] = element
	}
	if rules.isStruct(literal) {
		rules.breakList(literal.Lbrace, items, literal.Rbrace)
		return
	}
	// A slice, array, or map literal puts each element on its own line once
	// one of them spans lines.
	for _, element := range literal.Elts {
		value := element
		if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
			value = keyValue.Value
		}
		if rules.spansLines(value) {
			rules.breakList(literal.Lbrace, items, literal.Rbrace)
			return
		}
	}
}

func (rules *fileRules) fieldList(list *ast.FieldList) {
	if list == nil || len(list.List) == 0 || !list.Opening.IsValid() ||
		rules.line(list.Opening) == rules.line(list.Closing) {
		return
	}
	items := make([]ast.Node, len(list.List))
	for index, field := range list.List {
		items[index] = field
	}
	rules.breakList(list.Opening, items, list.Closing)
}

// ifStmt reports what is split inside an if header, and with FixHeaders
// moves it out.
func (rules *fileRules) ifStmt(statement *ast.IfStmt, isElseIf bool) {
	if statement.Init != nil && rules.initSpansHeader(statement.Init) {
		if !rules.canFixHeader(isElseIf) {
			if isElseIf {
				rules.report(statement.Init.Pos(), "if init spans lines in an else if; restructure it by hand")
			} else {
				rules.report(statement.Init.Pos(), "if init spans lines; assign it before the if")
			}
		} else if rules.moveInit(statement) {
			return
		} else {
			delete(rules.fixedIn, rules.function) // nothing moved; let another fix have this pass
		}
	}
	ast.Inspect(statement.Cond, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ParenExpr:
			if literal := rules.structLit(node.X); literal != nil {
				if rules.canFixHeader(isElseIf) {
					rules.extractLiteral(statement, node, literal)
				} else {
					rules.report(node.Pos(), "struct literal in an if condition; assign it to a variable first")
				}
				return false
			}
		case *ast.CallExpr:
			if rules.callSpansHeader(node) {
				rules.report(node.Pos(), "call spans lines in an if condition; assign its result first")
				return false
			}
		}
		return true
	})
}

// initSpansHeader ignores line breaks introduced inside a nested expression.
func (rules *fileRules) initSpansHeader(statement ast.Stmt) bool {
	assign, isAssign := statement.(*ast.AssignStmt)
	if !isAssign || len(assign.Rhs) == 0 {
		return rules.multiline(statement)
	}
	return rules.line(assign.TokPos) != rules.line(assign.Rhs[0].Pos())
}

// callSpansHeader ignores line breaks introduced inside a nested argument.
func (rules *fileRules) callSpansHeader(call *ast.CallExpr) bool {
	// A struct literal argument is split by this formatter; that split alone
	// should not make the surrounding if condition a header diagnostic.
	if rules.structLitArgument(call) {
		return false
	}
	if len(call.Args) == 0 {
		return rules.multiline(call)
	}
	if rules.line(call.Args[0].Pos()) != rules.line(call.Lparen) {
		return true
	}
	previous := call.Args[0].End()
	for _, argument := range call.Args[1:] {
		if rules.line(argument.Pos()) != rules.line(previous) {
			return true
		}
		previous = argument.End()
	}
	return false
}

func (rules *fileRules) structLitArgument(call *ast.CallExpr) bool {
	for _, argument := range call.Args {
		if rules.structLit(argument) != nil {
			return true
		}
	}
	return false
}

// moveInit turns "if init; cond {" into "init" and "if cond {". It reports
// whether it moved the init. It never changes which variable code refers to,
// so it refuses when the block the init moves to declares one of its
// variables later, or when one would shadow a variable outside the block.
func (rules *fileRules) moveInit(statement *ast.IfStmt) bool {
	init := string(rules.src[rules.offset(statement.Init.Pos()):rules.offset(statement.Init.End())])
	if assign, isAssign := statement.Init.(*ast.AssignStmt); isAssign && assign.Tok == token.DEFINE {
		reuse, shadows, conflict := rules.declarations(assign, statement)
		if conflict != "" {
			rules.report(
				statement.Init.Pos(),
				"if init spans lines, and moving it before the if would redeclare %s, "+
					"which this block declares later; fix it by hand",
				conflict,
			)
			return false
		}
		if len(shadows) > 0 {
			rules.report(
				statement.Init.Pos(),
				"if init spans lines, and moving it before the if would make %s "+
					"shadow, or be shadowed by, another variable of that name; fix it by hand",
				strings.Join(shadows, ", "),
			)
			return false
		}
		if reuse {
			at := rules.offset(assign.TokPos) - rules.offset(statement.Init.Pos())
			init = init[:at] + "=" + init[at+len(":="):]
		}
	}
	rules.changes = append(
		rules.changes,
		change{
			start: rules.offset(statement.Pos()),
			end:   rules.offset(statement.Cond.Pos()),
			text:  init + "\nif ",
			group: rules.newGroup(),
		},
	)
	return true
}

// declarations looks at the variables an "if x := ...;" init declares, in the
// block the init moves to. reuse means each already exists earlier in that
// block, with the same type, so the moved init must use =. shadows lists
// those that exist only outside the block, which a moved := now shadows.
// conflict names one the block declares later, or with another type, which
// makes the move impossible.
func (rules *fileRules) declarations(
	assign *ast.AssignStmt,
	statement *ast.IfStmt,
) (bool, []string, string) {
	if rules.pkg == nil {
		return false, nil, "a variable (no type information)"
	}
	block := rules.pkg.Scope().Innermost(statement.Pos())
	if block == nil {
		return false, nil, "a variable (no scope information)"
	}
	// The if statement has its own scope; the init moves to the one around it.
	if parent := block.Parent(); parent != nil && block.Pos() == statement.Pos() {
		block = parent
	}
	reused, declared := 0, 0
	var shadows []string
	for _, left := range assign.Lhs {
		ident, isIdent := left.(*ast.Ident)
		if !isIdent || ident.Name == "_" {
			continue
		}
		object := rules.info.Defs[ident]
		if object == nil {
			continue
		}
		declared++
		if existing := block.Lookup(ident.Name); existing != nil {
			variable, isVar := existing.(*types.Var)
			if !isVar || existing.Pos() > statement.Pos() || !types.Identical(variable.Type(), object.Type()) {
				return false, nil, ident.Name
			}
			reused++
			continue
		}
		if parent := block.Parent(); parent != nil {
			if _, outer := parent.LookupParent(ident.Name, statement.Pos()); outer != nil {
				if variable, isVar := outer.(*types.Var); isVar && variable.Parent() != rules.pkg.Scope() {
					shadows = append(shadows, ident.Name)
					continue
				}
			}
		}
		// The new variable would be shadowed by a later declaration in a
		// scope nested in the block, such as another "if x := ...".
		if rules.declaredLaterWithin(block, ident.Name, statement.End()) {
			shadows = append(shadows, ident.Name)
		}
	}
	return declared > 0 && reused == declared, shadows, ""
}

// declaredLaterWithin reports whether a scope nested in scope, starting at or
// after from, declares name. A multi-line if init that declares it does not
// count: it moves too, and then reuses the variable.
func (rules *fileRules) declaredLaterWithin(scope *types.Scope, name string, from token.Pos) bool {
	for index := range scope.NumChildren() {
		child := scope.Child(index)
		if child.End() <= from {
			continue
		}
		if child.Pos() >= from && child.Lookup(name) != nil && !rules.movesToo(child) {
			return true
		}
		if rules.declaredLaterWithin(child, name, from) {
			return true
		}
	}
	return false
}

// movesToo reports whether scope is the scope of an if statement whose
// multi-line init a later pass moves out.
func (rules *fileRules) movesToo(scope *types.Scope) bool {
	for node, nodeScope := range rules.info.Scopes {
		if nodeScope != scope {
			continue
		}
		statement, isIf := node.(*ast.IfStmt)
		return isIf && statement.Init != nil && (rules.multiline(statement.Init) || rules.hasStructLit(statement.Init))
	}
	return false
}

// hasStructLit reports a struct literal with fields in node, which a pass
// splits over lines.
func (rules *fileRules) hasStructLit(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if expression, isExpr := inner.(ast.Expr); isExpr && rules.structLit(expression) != nil {
			found = true
		}
		return !found
	})
	return found
}

// extractLiteral moves "(T{...})" out of an if condition into "want := T{...}"
// before the if statement.
func (rules *fileRules) extractLiteral(statement *ast.IfStmt, paren *ast.ParenExpr, literal *ast.CompositeLit) {
	typeName := "Value"
	switch typ := literal.Type.(type) {
	case *ast.Ident:
		typeName = typ.Name
	case *ast.SelectorExpr:
		typeName = typ.Sel.Name
	}
	variable := rules.freeName(statement.Pos(), "want", "want"+strings.ToUpper(typeName[:1])+typeName[1:])
	text := string(rules.src[rules.offset(literal.Pos()):rules.offset(literal.End())])
	group := rules.newGroup()
	rules.changes = append(
		rules.changes,
		change{
			start: rules.offset(paren.Pos()),
			end:   rules.offset(paren.End()),
			text:  variable,
			group: group,
		},
		change{
			start: rules.offset(statement.Pos()),
			end:   rules.offset(statement.Pos()),
			text:  variable + " := " + text + "\n",
			group: group,
		},
	)
}

// freeName returns the first candidate no identifier in pos's function uses.
func (rules *fileRules) freeName(pos token.Pos, candidates ...string) string {
	used := map[string]bool{}
	if rules.pkg != nil {
		if scope := rules.pkg.Scope().Innermost(pos); scope != nil {
			for walk := scope; walk != nil && walk != rules.pkg.Scope(); walk = walk.Parent() {
				for _, name := range walk.Names() {
					used[name] = true
				}
			}
		}
	}
	for _, ident := range rules.identsInFunction(pos) {
		used[ident] = true
	}
	for _, candidate := range candidates {
		if !used[candidate] {
			return candidate
		}
	}
	last := candidates[len(candidates)-1]
	for number := 2; ; number++ {
		if name := fmt.Sprintf("%s%d", last, number); !used[name] {
			return name
		}
	}
}

// identsInFunction lists the identifiers of the function around pos, so a
// new variable never shadows one used later in it.
func (rules *fileRules) identsInFunction(pos token.Pos) []string {
	var names []string
	for ident := range rules.info.Defs {
		if ident != nil && rules.withinFunction(ident.Pos(), pos) {
			names = append(names, ident.Name)
		}
	}
	for ident := range rules.info.Uses {
		if rules.withinFunction(ident.Pos(), pos) {
			names = append(names, ident.Name)
		}
	}
	sort.Strings(names)
	return names
}

func (rules *fileRules) withinFunction(ident, pos token.Pos) bool {
	if rules.pkg == nil {
		return false
	}
	scope := rules.pkg.Scope().Innermost(pos)
	for scope != nil && scope.Parent() != nil && scope.Parent() != rules.pkg.Scope() &&
		scope.Parent().Parent() != rules.pkg.Scope() {
		scope = scope.Parent()
	}
	return scope != nil && scope.Contains(ident)
}

// check walks a file and collects its changes and diagnostics.
func (rules *fileRules) check(file *ast.File) {
	elseIfs := map[*ast.IfStmt]bool{}
	rules.fixedIn = map[ast.Node]bool{}
	for _, declaration := range file.Decls {
		rules.function = declaration
		rules.checkNode(declaration, elseIfs)
	}
}

func (rules *fileRules) checkNode(root ast.Node, elseIfs map[*ast.IfStmt]bool) {
	ast.Inspect(root, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.IfStmt:
			if elseIf, isIf := node.Else.(*ast.IfStmt); isIf {
				elseIfs[elseIf] = true
			}
			rules.ifStmt(node, elseIfs[node])
		case *ast.ForStmt:
			if node.Cond != nil {
				ast.Inspect(node.Cond, func(inner ast.Node) bool {
					if call, isCall := inner.(*ast.CallExpr); isCall && rules.multiline(call) {
						rules.report(call.Pos(), "call spans lines in a for condition; assign its result first")
						return false
					}
					return true
				})
			}
		case *ast.AssignStmt:
			if len(node.Lhs) > 1 && len(node.Rhs) > 1 && rules.multiline(node) {
				rules.report(node.Pos(), "multi-value assignment spans lines; assign one variable per line")
			}
		case *ast.CompositeLit:
			rules.compositeLit(node)
		case *ast.CallExpr:
			rules.call(node)
		case *ast.FuncType:
			rules.fieldList(node.Params)
			rules.fieldList(node.Results)
		}
		return true
	})
}

// apply returns src with changes applied. A group that overlaps a change
// already applied is left whole for the next pass.
func apply(src []byte, changes []change) []byte {
	groups := map[int][]change{}
	for _, item := range changes {
		groups[item.group] = append(groups[item.group], item)
	}
	order := make([]int, 0, len(groups))
	highest := map[int]int{}
	for id, members := range groups {
		order = append(order, id)
		sort.SliceStable(members, func(i, j int) bool { return members[i].start > members[j].start })
		groups[id] = members
		highest[id] = members[0].end
	}
	sort.Slice(order, func(i, j int) bool {
		if highest[order[i]] != highest[order[j]] {
			return highest[order[i]] > highest[order[j]]
		}
		return order[i] < order[j]
	})
	out := append([]byte{}, src...)
	lowest := len(src) + 1
	seen := map[string]bool{}
	for _, id := range order {
		members := groups[id]
		if members[0].end > lowest {
			continue
		}
		key := fmt.Sprint(members)
		if seen[key] {
			continue
		}
		seen[key] = true
		for _, item := range members {
			out = append(out[:item.start], append([]byte(item.text), out[item.end:]...)...)
		}
		lowest = members[len(members)-1].start
	}
	return out
}
