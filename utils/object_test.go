package utils_test

import (
	"testing"

	"github.com/hexlinevault/core-api.git/helpers/dump"
	"github.com/hexlinevault/core-api.git/utils"
)

type TestNested struct {
	Value *float64
}

type TestOuter struct {
	Nested *TestNested
}

func TestNestedValue(t *testing.T) {
	val := 123.45
	outer := &TestOuter{
		Nested: &TestNested{
			Value: &val,
		},
	}

	// 1. Test successful struct path
	if utils.NestedValue(outer, "Nested.Value", 0.0) != 123.45 {
		t.Errorf("Expected 123.45, got %v", utils.NestedValue(outer, "Nested.Value", 0.0))
	}

	// 2. Test nil pointer in middle
	outerNil := &TestOuter{Nested: nil}
	if utils.NestedValue(outerNil, "Nested.Value", 99.0) != 99.0 {
		t.Errorf("Expected 99.0, got %v", utils.NestedValue(outerNil, "Nested.Value", 99.0))
	}

	// 3. Test map access
	dataMap := map[string]interface{}{
		"Config": map[string]float64{
			"Rate": 1.5,
		},
	}
	if utils.NestedValue(dataMap, "Config.Rate", 0.0) != 1.5 {
		t.Errorf("Expected 1.5 from map, got %v", utils.NestedValue(dataMap, "Config.Rate", 0.0))
	}

	// 4. Test slice access
	items := []map[string]int{
		{"ID": 10},
		{"ID": 20},
	}
	if utils.NestedValue(items, "1.ID", 0) != 20 {
		t.Errorf("Expected 20 from slice index 1, got %v", utils.NestedValue(items, "1.ID", 0))
	}

	// 5. Test type conversion (float64 requested, int provided)
	if utils.NestedValue(items, "0.ID", 0.0) != 10.0 {
		t.Errorf("Expected 10.0 (float64), got %T: %v", utils.NestedValue(items, "0.ID", 0.0), utils.NestedValue(items, "0.ID", 0.0))
	}

	// 6. Test with the user's specific scenario concept
	type CGTransactionAdditional struct {
		Privilege float64
		Test      *string
	}
	type CGTransaction struct {
		Additional *CGTransactionAdditional
	}
	type CGResponse struct {
		Data *CGTransaction
	}

	a := CGResponse{
		Data: &CGTransaction{
			Additional: &CGTransactionAdditional{
				Privilege: 10.5,
				Test:      nil,
			},
		},
	}

	// 7. Test GetNestedValue (Success)
	res, err := utils.GetNestedValue[*CGTransactionAdditional](a, "Data.Additional")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.Privilege != 10.5 {
		t.Errorf("Expected 10.5, got %v", res.Privilege)
	}

	// 8. Test GetNestedValue (Error - Invalid Path)
	_, err = utils.GetNestedValue[float64](a, "Data.InvalidField.Something")
	if err == nil {
		t.Errorf("Expected error for invalid path, got nil")
	} else {
		dump.DD("Expected Error Message:", err.Error())
	}

	// 9. Test NestedValue with nil field at the end
	res2 := utils.NestedValue[*string](a, "Data.Additional.Test", utils.Pointer("test"))
	if *res2 != "test" {
		t.Errorf("Expected fallback 'test', got %v", *res2)
	}

	// 10. Test GetNestedValue (Error - Invalid Type)
	_, err = utils.GetNestedValue[string](a, "Data.Additional.Privilege")
	if err == nil {
		t.Errorf("Expected error for invalid type")
	} else {
		dump.DD("Expected Error Message:", err.Error())
	}

	// 11. Test GetNestedValue (Error - Invalid Type)
	_, err = utils.GetNestedValue[string](a, "Data.Additional.Test")
	if err == nil {
		t.Errorf("Expected error for invalid type")
	} else {
		dump.DD("Expected Error Message:", err.Error())
	}
}
