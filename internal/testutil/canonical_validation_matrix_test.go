package testutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type canonicalValidationExpectation struct {
	filePath    string
	testName    string
	callName    string
	sectionName string
}

func TestCanonicalValidationMatrixEntriesEnforced(t *testing.T) {
	root := findProjectRoot(t)
	matrixPath := filepath.Join(root, "docs", "testing", "canonical_log_validation_matrix.md")
	matrixBytes, readErr := os.ReadFile(matrixPath)
	if readErr != nil {
		t.Fatalf("read canonical validation matrix: %v", readErr)
	}

	expectations := parseCanonicalValidationMatrix(t, string(matrixBytes))
	if len(expectations) == 0 {
		t.Fatal("canonical validation matrix did not yield any expectations")
	}

	fileSet := token.NewFileSet()
	parsedFiles := make(map[string]*ast.File)

	for _, expectation := range expectations {
		absPath := filepath.Join(root, filepath.FromSlash(expectation.filePath))
		fileNode, ok := parsedFiles[absPath]
		if !ok {
			parsed, parseErr := parser.ParseFile(fileSet, absPath, nil, 0)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", expectation.filePath, parseErr)
			}
			fileNode = parsed
			parsedFiles[absPath] = fileNode
		}

		testFunc := findNamedFunc(fileNode, expectation.testName)
		if testFunc == nil {
			t.Fatalf("%s: missing test %s referenced by canonical validation matrix", expectation.filePath, expectation.testName)
		}
		if !funcContainsCall(testFunc, expectation.callName) {
			t.Fatalf("%s: %s is listed under %s but does not call %s", expectation.filePath, expectation.testName, expectation.sectionName, expectation.callName)
		}
	}
}

func parseCanonicalValidationMatrix(t *testing.T, markdown string) []canonicalValidationExpectation {
	t.Helper()

	const (
		directSection = "Direct validation call sites"
		helperSection = "Helper-wrapped validation call sites"
	)

	rowPattern := regexp.MustCompile("^\\| `([^`]+)` \\| `([^`]+)` \\|$")
	lines := strings.Split(markdown, "\n")
	expectations := make([]canonicalValidationExpectation, 0)
	sectionName := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "## " + directSection:
			sectionName = directSection
			continue
		case "## " + helperSection:
			sectionName = helperSection
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			sectionName = ""
			continue
		}

		matches := rowPattern.FindStringSubmatch(trimmed)
		if len(matches) != 3 || sectionName == "" {
			continue
		}

		callName := "ValidateCanonicalLogs"
		if sectionName == helperSection {
			callName = "validateCanonicalLogsWithFailureCheck"
		}

		expectations = append(expectations, canonicalValidationExpectation{
			filePath:    matches[1],
			testName:    matches[2],
			callName:    callName,
			sectionName: sectionName,
		})
	}

	return expectations
}

func findNamedFunc(fileNode *ast.File, name string) *ast.FuncDecl {
	for _, decl := range fileNode.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name == nil {
			continue
		}
		if funcDecl.Name.Name == name {
			return funcDecl
		}
	}
	return nil
}

func funcContainsCall(funcDecl *ast.FuncDecl, callName string) bool {
	if funcDecl == nil || funcDecl.Body == nil {
		return false
	}
	found := false
	ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch fun := callExpr.Fun.(type) {
		case *ast.Ident:
			if fun.Name == callName {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			if fun.Sel != nil && fun.Sel.Name == callName {
				found = true
				return false
			}
		}

		return true
	})
	return found
}
