package parser

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/cockroach/sql/parser"
	"github.com/davecgh/go-spew/spew"
)

func init() {
	spew.Config.DisableMethods = true
}

type visitorFunc func(parser.Expr) parser.Expr

func (v visitorFunc) Visit(e parser.Expr) parser.Expr {
	return v(e)
}

// Fuzz is the entry point for go-fuzz. Run it via
//	  go-fuzz-build github.com/cockroachdb/go-fuzz/examples/parser && \
//    go-fuzz -bin=./parser-fuzz.zip -workdir=.
func Fuzz(data []byte) (interestingness int) {
	sql := string(data)
	stmts, err := parser.Parse(sql)
	if err != nil || stmts == nil {
		if stmts != nil {
			panic("stmt is not nil on error")
		}
		return
	}
	for _, stmt := range stmts {
		interestingness = fuzzSingle(stmt)
	}
	return
}

type nullEnv struct{}

func (nullEnv) Get(_ string) (parser.Datum, bool) {
	return parser.DNull{}, true
}

var env = nullEnv{}

func expected(str string) bool {
	for _, substr := range []string{
		"ParseFloat",
		"unknown function",
		"cannot convert",
		"zero modulus",
		"incorrect number",
		"argument type mismatch",
		"unexpected expression",
		"operator",      // unsupported (unary|binary|...) operator
		"not supported", // octal, [...] not supported
		"TODO",          // TODO(pmattis): LIKE unimplemented (etc)
		"unexpected expression",
		"eval: unsupported expression type: *parser.StarExpr", // #1948
		"walk: unsupported expression type: <nil>",            // #1949

		// past trophies:
		// `DATABASE`,                    // # 1818
		// `syntax error at or near ")"`, // #1817
		// "interface is nil, not",       // probably since sql.y ignores unimplemented bits
		// `*`, // #1810. Just disencourage * use in general for now.
	} {
		if strings.Contains(str, substr) {
			return true
		}
	}
	return false
}

func fuzzSingle(stmt parser.Statement) (interestingness int) {
	var lastExpr parser.Expr
	rcvr := func() {
		if r := recover(); r != nil {
			if !expected(fmt.Sprintf("%v", r)) {
				fmt.Printf("Stmt: %s\n%s", stmt, spew.Sdump(stmt))
				if lastExpr != nil {
					fmt.Printf("Expr: %s", spew.Sdump(lastExpr))
				}
				panic(r)
			}
			// Anything that has expected errors in it is fine, but not as
			// interesting as things that go through.
			interestingness = 1
		}
	}
	defer rcvr()

	data0 := stmt.String()
	// TODO(tschottdorf): again, this is since we're ignoring stuff in the
	// grammar instead of erroring out on unsupported language. See:
	// https://github.com/cockroachdb/cockroach/issues/1949
	if strings.Contains(data0, "%!s(<nil>)") {
		return 0
	}
	stmt1, err := parser.Parse(data0)
	if err != nil {
		fmt.Printf("AST: %s", spew.Sdump(stmt))
		fmt.Printf("data0: %q\n", data0)
		panic(err)
	}
	interestingness = 2

	data1 := stmt1.String()
	// TODO(tschottdorf): due to the ignoring issue again.
	// if !reflect.DeepEqual(stmt, stmt1) {
	if data1 != data0 {
		fmt.Printf("data0: %q\n", data0)
		fmt.Printf("AST: %s", spew.Sdump(stmt))
		fmt.Printf("data1: %q\n", data1)
		fmt.Printf("AST: %s", spew.Sdump(stmt1))
		panic("not equal")
	}

	var v visitorFunc = func(e parser.Expr) parser.Expr {
		lastExpr = e
		if _, err := parser.EvalExpr(e, env); err != nil {
			panic(err)
		}
		return e
	}
	parser.WalkStmt(v, stmt)
	return
}
