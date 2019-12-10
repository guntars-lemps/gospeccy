// GoSpeccy's scripting language based on "github.com/sbinet/go-eval"
package interpreter

import (
	"fmt"
	"github.com/guntars-lemps/gospeccy/spectrum"
	"github.com/sbinet/go-eval"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
)

// ==============
// Some variables
// ==============

// These variables are set only once, before starting new goroutines,
// so there is no need for controlling concurrent access via a sync.Mutex
var (
	app        *spectrum.Application
	cmdLineArg string // The 1st non-flag command-line argument, or empty string
	speccy     *spectrum.Spectrum48k
	w          *eval.World
	intp       *Interpreter = newInterpreter()
	stdout     io.Writer    = os.Stdout
)

var mutex sync.Mutex

const (
	SCRIPT_DIRECTORY = "scripts"
	STARTUP_SCRIPT   = "startup"
)

type Interpreter struct {
	// The set of top-level Go variables.
	// (This is a set, the values associated with the keys are pointless.)
	vars map[string]bool
}

func newInterpreter() *Interpreter {
	return &Interpreter{
		vars: make(map[string]bool),
	}
}

// Returns the previous stdout
func (i *Interpreter) SetStdout(newStdout io.Writer) io.Writer {
	mutex.Lock()
	defer mutex.Unlock()

	old := stdout
	stdout = newStdout
	return old
}

func (i *Interpreter) Run(sourceCode string) error {
	sourceCode = strings.TrimSpace(sourceCode)
	if sourceCode == "" {
		sourceCode = "help()"
	}

	err := i.run(w, "", sourceCode)

	return err
}

type ast_state_t int

const (
	NONE           ast_state_t = iota
	DEFINE_VAR_LHS             // The left-hand-side of an assignment which defines a new variable
)

type ast_visitor_t struct {
	newVars map[string]bool
	state   ast_state_t
}

func (v *ast_visitor_t) Visit(node ast.Node) ast.Visitor {
	switch v.state {
	case NONE:
		switch n := node.(type) {
		case *ast.AssignStmt:
			{
				if n.Tok == token.DEFINE {
					// Walk the left-hand-side to find names of the variables
					oldState := v.state
					v.state = DEFINE_VAR_LHS
					for _, lhs := range n.Lhs {
						ast.Walk(v, lhs)
					}
					v.state = oldState
				}

				return nil
			}

		case *ast.DeclStmt:
			return v

		case *ast.GenDecl:
			return v

		case *ast.ValueSpec:
			{
				var ident *ast.Ident
				for _, ident = range n.Names {
					v.newVars[ident.Name] = true
				}
				return nil
			}

		default:
			//fmt.Printf("%#v\n", node);
			return nil
		}

	case DEFINE_VAR_LHS:
		if ident, isIdent := node.(*ast.Ident); isIdent {
			v.newVars[ident.Name] = true
			return nil
		} else {
			return v
		}
	}

	//fmt.Printf("%#v\n", node);
	return nil
}

// Adds declarations of top-level variables to 'buffer'
func addTopLevelVars(node ast.Node, buffer map[string]bool) {
	v := &ast_visitor_t{
		newVars: buffer,
		state:   NONE,
	}
	ast.Walk(v, node)
}

func parseStmtList(fset *token.FileSet, src string) ([]ast.Stmt, error) {
	f, err := parser.ParseFile(fset, "input", "package p;func _(){"+src+"\n}", 0)
	if err != nil {
		return nil, err
	}
	return f.Decls[0].(*ast.FuncDecl).Body.List, nil
}

func parseDeclList(fset *token.FileSet, src string) ([]ast.Decl, error) {
	f, err := parser.ParseFile(fset, "input", "package p;"+src, 0)
	if err != nil {
		return nil, err
	}
	return f.Decls, nil
}

// Parse and compile the specified source code.
// If the code was successfully compiled, 'err' is nil.
//
// The output parameter 'vars' contains the names of new top-level
// variables potentially defined by the source code.
// 'vars' may contain some elements even if an error occurred.
func (i *Interpreter) compile(w *eval.World, fileSet *token.FileSet, sourceCode string) (code eval.Code, vars []string, err error) {
	var statements []ast.Stmt
	var declarations []ast.Decl

	vars_buffer := make(map[string]bool)

	statements, err1 := parseStmtList(fileSet, sourceCode)
	if err1 == nil {
		for _, s := range statements {
			addTopLevelVars(s, vars_buffer)
		}
		vars = make([]string, 0, len(vars_buffer))
		for varName := range vars_buffer {
			vars = append(vars, varName)
		}

		code, err = w.CompileStmtList(fileSet, statements)

		return code, vars, err
	}

	declarations, err2 := parseDeclList(fileSet, sourceCode)
	if err2 == nil {
		for _, d := range declarations {
			addTopLevelVars(d, vars_buffer)
		}
		vars = make([]string, 0, len(vars_buffer))
		for varName := range vars_buffer {
			vars = append(vars, varName)
		}

		code, err = w.CompileDeclList(fileSet, declarations)

		return code, vars, err
	}

	return nil, nil, err1
}

// Examines whether 'w' has values for the variables in 'vars'.
// For each successfully found/verified variable, the variable's name is added to 'i.vars'.
func (i *Interpreter) tryToAddVars(w *eval.World, fileSet *token.FileSet, vars []string) {
	for _, name := range vars {
		_, err := w.Compile(fileSet, name /*sourceCode*/)
		if err == nil {
			// The variable exists, add its name to 'i.vars'
			i.vars[name] = true
		} else {
			// Ignore the error. Conclude that no such variable exists.
		}
	}
}

// Runs the specified Go source code in the context of 'w'
func (i *Interpreter) run(w *eval.World, path_orEmpty string, sourceCode string) error {
	var code eval.Code
	var vars []string
	var err error

	fileSet := token.NewFileSet()
	if len(path_orEmpty) > 0 {
		fileSet.AddFile(path_orEmpty, fileSet.Base(), len(sourceCode))
	}

	code, vars, err = i.compile(w, fileSet, sourceCode)
	i.tryToAddVars(w, fileSet, vars)
	if err != nil {
		return err
	}

	result, err := code.Run()
	if err != nil {
		return err
	}

	if result != nil {
		fmt.Fprintf(stdout, "%s\n", result)
	}

	return nil
}

// Loads and evaluates the specified Go script
func runScript(w *eval.World, scriptName string, optional bool) error {
	fileName := scriptName + ".go"

	path, err := spectrum.ScriptPath(fileName)
	if err != nil {
		return err
	}

	scriptData, err := ioutil.ReadFile(path)
	if err != nil {
		if !optional {
			return err
		} else {
			return nil
		}
	}

	err = intp.run(w, fileName, string(scriptData))
	return err
}

func Init(_app *spectrum.Application, _cmdLineArg string, _speccy *spectrum.Spectrum48k) {
	app = _app
	cmdLineArg = _cmdLineArg
	speccy = _speccy

	if w == nil {
		w = eval.NewWorld()
		defineFunctions(w)
	}
}

func GetInterpreter() *Interpreter {
	return intp
}
