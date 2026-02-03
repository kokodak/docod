package extractor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractor_ExtractFromFile(t *testing.T) {
	testFile := filepath.Join("testdata", "sample.go")

	ext, err := NewExtractor("go")
	require.NoError(t, err)

	units, err := ext.ExtractFromFile(testFile)
	require.NoError(t, err)

	// Group units by name for easier lookup
	unitsByName := make(map[string]*CodeUnit)
	for _, unit := range units {
		unitsByName[unit.Name] = unit
	}

	t.Run("Overall Count", func(t *testing.T) {
		// Base, User, Handler, MyFunc, MyFunction, MyMethod, Version, StatusOK, StatusError, GlobalVar
		assert.Equal(t, 10, len(units))
	})

	t.Run("Body Relations and Sanitization", func(t *testing.T) {
		// Check MyFunc calls MyFunction
		myFunc := unitsByName["MyFunc"]
		require.NotNil(t, myFunc)
		var foundCall bool
		for _, rel := range myFunc.Relations {
			if rel.Target == "MyFunction" && rel.Kind == "calls" {
				foundCall = true
			}
		}
		assert.True(t, foundCall, "MyFunc should call MyFunction")

		// Check MyMethod instantiates Base
		myMethod := unitsByName["MyMethod"]
		require.NotNil(t, myMethod)
		var foundBaseInstantiation bool
		var foundFmtCall bool
		var foundMakeCall bool
		for _, rel := range myMethod.Relations {
			if rel.Target == "Base" && rel.Kind == "instantiates" {
				foundBaseInstantiation = true
			}
			if strings.HasPrefix(rel.Target, "fmt.") {
				foundFmtCall = true
			}
			if rel.Target == "make" {
				foundMakeCall = true
			}
		}
		assert.True(t, foundBaseInstantiation, "MyMethod should instantiate Base")
		assert.False(t, foundFmtCall, "Standard library 'fmt' should be sanitized")
		assert.False(t, foundMakeCall, "Built-in 'make' should be sanitized")
	})

	t.Run("Package Name", func(t *testing.T) {
		for _, unit := range units {
			assert.Equal(t, "sample", unit.Package, "Each unit should have the correct package name")
		}
	})

	t.Run("Constants", func(t *testing.T) {
		unit, ok := unitsByName["Version"]
		require.True(t, ok)
		assert.Equal(t, "constant", unit.UnitType)
		assert.Equal(t, "Version is the application version.", unit.Description)
		details := unit.Details.(GoConstDetails)
		assert.Equal(t, "\"1.0.0\"", details.Value)

		unit, ok = unitsByName["StatusOK"]
		require.True(t, ok)
		assert.Equal(t, "StatusOK indicates success.", unit.Description)
		details = unit.Details.(GoConstDetails)
		assert.Equal(t, "200", details.Value)
	})

	t.Run("Variables", func(t *testing.T) {
		unit, ok := unitsByName["GlobalVar"]
		require.True(t, ok)
		assert.Equal(t, "variable", unit.UnitType)
		assert.Equal(t, "GlobalVar is a global variable.", unit.Description)
		details := unit.Details.(GoVarDetails)
		assert.Equal(t, "\"hello\"", details.Value)
	})

	t.Run("Base Struct", func(t *testing.T) {
		unit, ok := unitsByName["Base"]
		require.True(t, ok, "Base struct should be found")
		assert.Equal(t, "struct", unit.UnitType)

		details, ok := unit.Details.(GoTypeDetails)
		require.True(t, ok)
		assert.Len(t, details.Fields, 1)
		assert.Equal(t, "ID", details.Fields[0].Name)
		assert.Equal(t, "int", details.Fields[0].Type)
	})

	t.Run("User Struct", func(t *testing.T) {
		unit, ok := unitsByName["User"]
		require.True(t, ok, "User struct should be found")
		assert.Equal(t, "struct", unit.UnitType)

		details, ok := unit.Details.(GoTypeDetails)
		require.True(t, ok)
		// Base (embedded), Name, Nickname, Age
		assert.Len(t, details.Fields, 4)

		// Check embedded field
		assert.Equal(t, "Base", details.Fields[0].Name)
		assert.Equal(t, "Base", details.Fields[0].Type)

		// Check field with multiple identifiers
		assert.Equal(t, "Name", details.Fields[1].Name)
		assert.Equal(t, "Nickname", details.Fields[2].Name)
		assert.Equal(t, "string", details.Fields[1].Type)
		assert.Contains(t, details.Fields[1].Tag, `json:"name"`)
	})

	t.Run("Handler Interface", func(t *testing.T) {
		unit, ok := unitsByName["Handler"]
		require.True(t, ok, "Handler interface should be found")
		assert.Equal(t, "interface", unit.UnitType)

		details, ok := unit.Details.(GoInterfaceDetails)
		require.True(t, ok)
		// fmt.Stringer (embedded), Handle, Close
		assert.Len(t, details.Methods, 3)

		var foundHandle bool
		for _, m := range details.Methods {
			if strings.HasPrefix(m.Signature, "Handle") {
				foundHandle = true
				assert.Len(t, m.Parameters, 2)
				assert.Len(t, m.Returns, 2)
				assert.Equal(t, "ctx", m.Parameters[0].Name)
				assert.Equal(t, "string", m.Parameters[0].Type)
			}
		}
		assert.True(t, foundHandle, "Handle method should be extracted with details")
	})

	t.Run("Functions", func(t *testing.T) {
		unit, ok := unitsByName["MyFunc"]
		require.True(t, ok, "MyFunc should be found")
		assert.Equal(t, "function", unit.UnitType)

		details, ok := unit.Details.(GoFunctionDetails)
		require.True(t, ok)
		assert.Len(t, details.Parameters, 2)
		assert.Len(t, details.Returns, 1)
		assert.Equal(t, "bool", details.Returns[0].Type)
	})

	t.Run("Methods", func(t *testing.T) {
		unit, ok := unitsByName["MyMethod"]
		require.True(t, ok, "MyMethod should be found")
		assert.Equal(t, "method", unit.UnitType)

		details, ok := unit.Details.(GoFunctionDetails)
		require.True(t, ok)
		assert.NotEmpty(t, details.Receiver)
		assert.Contains(t, details.Receiver, "*User")
	})
}
