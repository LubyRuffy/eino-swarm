package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
)

// storedEventKinds lists every event kind the engine and store write: Kind*
// string constants plus literal `Kind: "..."` fields in event values.
func storedEventKinds(t *testing.T) map[string]string {
	t.Helper()
	kinds := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range []string{".", filepath.Join("..", "store")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				var name string
				var value ast.Expr
				switch n := node.(type) {
				case *ast.ValueSpec:
					if len(n.Names) != 1 || len(n.Values) != 1 || !strings.HasPrefix(n.Names[0].Name, "Kind") {
						return true
					}
					name, value = n.Names[0].Name, n.Values[0]
				case *ast.KeyValueExpr:
					key, ok := n.Key.(*ast.Ident)
					if !ok || key.Name != "Kind" {
						return true
					}
					name, value = "Kind field", n.Value
				default:
					return true
				}
				literal, ok := value.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				kind, err := strconv.Unquote(literal.Value)
				if err == nil && kind != "" {
					kinds[kind] = fset.Position(literal.Pos()).String() + " " + name
				}
				return true
			})
		}
	}
	for k := swarm.NotifyKind(0); k.String() != "unknown"; k++ {
		kinds[k.String()] = "swarm." + k.String()
	}
	return kinds
}

// The desktop reducer shows a kind it does not recognise as a chat card of
// the event's raw text. A new internal receipt without an explicit decision
// there put raw query_history SQL into a conversation; every stored kind must
// be named by the reducer, either as a chat row or as Trace-only metadata.
func TestEveryStoredEventKindHasAnExplicitDesktopTranscriptDecision(t *testing.T) {
	kinds := storedEventKinds(t)
	if _, ok := kinds[KindHistoryReadStarted]; !ok {
		t.Fatalf("kind scan missed %s: %v", KindHistoryReadStarted, kinds)
	}
	if _, ok := kinds["history_registry_inherited"]; !ok {
		t.Fatal("kind scan missed store-written event literals")
	}
	sources, err := filepath.Glob(filepath.Join("..", "..", "frontend", "src", "lib", "transcript*.ts"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("transcript reducer sources: %v %v", sources, err)
	}
	var reducer strings.Builder
	for _, path := range sources {
		if strings.HasSuffix(path, ".test.ts") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		reducer.Write(raw)
	}
	for kind, origin := range kinds {
		if !strings.Contains(reducer.String(), strconv.Quote(kind)) {
			t.Errorf("event kind %q (%s) has no explicit desktop transcript decision", kind, origin)
		}
	}
}
