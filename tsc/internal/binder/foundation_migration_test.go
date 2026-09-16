package binder

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

func parseFoundationTestFile(fileName, source string, scriptKind core.ScriptKind) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: fileName, Path: tspath.Path(fileName)}, source, scriptKind)
}

func TestBindSourceFileSymbolMatchesParseRoot(t *testing.T) {
	t.Parallel()

	t.Run("external module", func(t *testing.T) {
		file := parseFoundationTestFile("/module.ts", "export const value = 1;\n", core.ScriptKindTS)
		BindSourceFile(file)

		rootSymbol := file.ParseRoot().Symbol()
		if rootSymbol == nil || rootSymbol != file.Symbol {
			t.Fatalf("parse root Symbol %p does not match SourceFile.Symbol %p", rootSymbol, file.Symbol)
		}
		if rootSymbol.Exports["value"] == nil {
			t.Fatal("expected the module export to remain on the root Symbol")
		}
	})

	t.Run("JSON restores module Symbol and keeps synthetic export", func(t *testing.T) {
		file := parseFoundationTestFile("/config.json", `{"name":"sample"}`, core.ScriptKindJSON)
		BindSourceFile(file)

		rootSymbol := file.ParseRoot().Symbol()
		if rootSymbol == nil || rootSymbol != file.Symbol {
			t.Fatalf("JSON parse root Symbol %p does not match restored SourceFile.Symbol %p", rootSymbol, file.Symbol)
		}
		property := file.Symbol.Exports[ast.InternalSymbolNameExportEquals]
		if property == nil {
			t.Fatalf("JSON binding did not retain the synthetic %q property in Exports", ast.InternalSymbolNameExportEquals)
		}
		if property == rootSymbol {
			t.Fatal("JSON synthetic property Symbol replaced the original module Symbol")
		}
		if property.Flags&ast.SymbolFlagsProperty == 0 {
			t.Fatalf("JSON synthetic export has flags %v; want SymbolFlagsProperty", property.Flags)
		}
	})
}

func TestBindFlowAttachmentsStayOnTheirOwnerNodes(t *testing.T) {
	t.Parallel()
	file := parseFoundationTestFile("/flow.ts", `
function endFlowTarget(flag: boolean) {
    if (flag) { return; }
}
class C {
    constructor(flag: boolean) { if (flag) return; }
    static { if (true) { const value = 1; } }
}
function switchTarget(value: number) {
    switch (value) {
        case 0: value++;
        case 1: break;
        default: break;
    }
}
`, core.ScriptKindTS)
	BindSourceFile(file)

	var functionNode, constructorNode, staticBlockNode, switchCaseFirst, switchCaseLast ast.Handle
	var caseNodes []ast.Handle
	ast.Walk(file.ParseRoot(), func(node ast.Handle) bool {
		switch node.Kind {
		case ast.KindFunctionDeclaration:
			if node.Name().Text() == "endFlowTarget" {
				functionNode = node
			}
		case ast.KindConstructor:
			constructorNode = node
		case ast.KindClassStaticBlockDeclaration:
			staticBlockNode = node
		case ast.KindCaseClause, ast.KindDefaultClause:
			caseNodes = append(caseNodes, node)
		}
		return false
	})
	if functionNode.IsNil() || constructorNode.IsNil() || staticBlockNode.IsNil() || len(caseNodes) != 3 {
		t.Fatalf("parsed expected flow owners: function=%v constructor=%v static block=%v cases=%d", !functionNode.IsNil(), !constructorNode.IsNil(), !staticBlockNode.IsNil(), len(caseNodes))
	}
	switchCaseFirst, switchCaseLast = caseNodes[0], caseNodes[len(caseNodes)-1]

	if functionNode.EndFlowNode() == nil {
		t.Fatal("function declaration is missing its EndFlow")
	}
	if body := functionNode.Body(); body.EndFlowNode() != nil {
		t.Fatal("EndFlow was attached to the function body instead of the function declaration")
	}
	if constructorNode.ReturnFlowNode() == nil {
		t.Fatal("constructor is missing its ReturnFlow")
	}
	if body := constructorNode.Body(); body.ReturnFlowNode() != nil {
		t.Fatal("ReturnFlow was attached to the constructor body instead of the constructor")
	}
	if staticBlockNode.EndFlowNode() != nil || staticBlockNode.Flags()&ast.NodeFlagsHasImplicitReturn != 0 {
		t.Fatal("static block has no upstream BodyBase and must not receive EndFlow or HasImplicitReturn")
	}
	if staticBlockNode.ReturnFlowNode() == nil {
		t.Fatal("class static block is missing its ReturnFlow")
	}
	if body := staticBlockNode.Body(); body.ReturnFlowNode() != nil {
		t.Fatal("ReturnFlow was attached to the static-block body instead of the static block")
	}
	if switchCaseFirst.FallthroughFlowNode() == nil {
		t.Fatal("reachable non-final case is missing its FallthroughFlow")
	}
	if switchCaseLast.FallthroughFlowNode() != nil {
		t.Fatal("final switch clause should not own a FallthroughFlow")
	}
}

func TestLookupNameReadsLocalsOnEveryContainerKind(t *testing.T) {
	t.Parallel()
	file := parseFoundationTestFile("/locals.ts", `
class ClassDeclaration<T> {}
const ClassExpression = class<T> {};
interface Call<T> { (value: T): T; }
interface Construct<T> { new (value: T): T; }
interface Index<T> { [key: string]: T; }
interface Method<T> { method<U>(value: T): U; }
type FunctionType<T> = (value: T) => T;
type ConstructorType<T> = new (value: T) => T;
`, core.ScriptKindTS)
	store := file.ParseStore()
	wantKinds := map[ast.Kind]bool{
		ast.KindClassDeclaration:   true,
		ast.KindClassExpression:    true,
		ast.KindFunctionType:       true,
		ast.KindConstructorType:    true,
		ast.KindCallSignature:      true,
		ast.KindConstructSignature: true,
		ast.KindIndexSignature:     true,
		ast.KindMethodSignature:    true,
	}
	seenKinds := make(map[ast.Kind]bool, len(wantKinds))
	b := &Binder{store: store}
	ast.Walk(file.ParseRoot(), func(node ast.Handle) bool {
		if !wantKinds[node.Kind] {
			return false
		}
		want := &ast.Symbol{Name: "locals-value"}
		store.SetLocals(node.Ref(), ast.SymbolTable{"lookup-target": want})
		if got := b.lookupName("lookup-target", node.Ref()); got != want {
			t.Errorf("lookupName skipped Locals on %v: got %p, want %p", node.Kind, got, want)
		}
		seenKinds[node.Kind] = true
		return false
	})
	for kind := range wantKinds {
		if !seenKinds[kind] {
			t.Errorf("fixture did not parse a %v node", kind)
		}
	}
}

func TestBindRepeatedAndPoolReuseDoNotShareFileState(t *testing.T) {
	t.Parallel()
	first := parseFoundationTestFile("/first.ts", "export const first = 1;\n", core.ScriptKindTS)
	BindSourceFile(first)
	firstSymbol := first.Symbol
	if firstSymbol == nil {
		t.Fatal("first module has no Symbol")
	}
	firstDeclarationCount := len(firstSymbol.Declarations)
	BindSourceFile(first)
	if first.Symbol != firstSymbol || first.ParseRoot().Symbol() != firstSymbol {
		t.Fatal("binding the same SourceFile twice changed its module Symbol")
	}
	if len(firstSymbol.Declarations) != firstDeclarationCount {
		t.Fatalf("second bind changed declaration count from %d to %d", firstDeclarationCount, len(firstSymbol.Declarations))
	}

	second := parseFoundationTestFile("/second.ts", "export const second = 2;\n", core.ScriptKindTS)
	BindSourceFile(second)
	if second.Symbol == nil || second.Symbol == firstSymbol {
		t.Fatal("pool reuse gave the second SourceFile the first file's module Symbol")
	}
	if second.Symbol.Exports["second"] == nil || second.Symbol.Exports["first"] != nil {
		t.Fatalf("second module has unexpected exports: first=%p second=%p", second.Symbol.Exports["first"], second.Symbol.Exports["second"])
	}
	if firstSymbol.Exports["first"] == nil || firstSymbol.Exports["second"] != nil {
		t.Fatalf("first module's exports changed after binding the second file: first=%p second=%p", firstSymbol.Exports["first"], firstSymbol.Exports["second"])
	}
}
