package utils

import (
	"reflect"
	"testing"
)

func TestOmit(t *testing.T) {
	type TestStruct struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Admin bool   `json:"-"`
	}

	t.Run("omit from struct", func(t *testing.T) {
		s := TestStruct{ID: 1, Name: "User", Email: "user@example.com", Admin: true}
		res := Omit(s, "email", "id")

		expected := map[string]any{
			"name": "User",
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("omit from embedded struct (flattened)", func(t *testing.T) {
		type Embedded struct {
			ID int `json:"id"`
		}
		type TestWithEmbedded struct {
			Embedded
			Name string `json:"name"`
		}

		s := TestWithEmbedded{
			Embedded: Embedded{ID: 100},
			Name:     "Tester",
		}
		res := Omit(s, "name")

		expected := map[string]any{
			"id": 100,
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("omit from complex struct (mixed embedded and relations)", func(t *testing.T) {
		type BaseUser struct {
			ID   int    `json:"id"`
			Role string `json:"role"`
		}
		type User struct {
			BaseUser
			AgentUser *BaseUser `json:"agent_user,omitempty"`
		}

		s := User{
			BaseUser: BaseUser{ID: 1, Role: "player"},
			AgentUser: &BaseUser{ID: 99, Role: "agent"},
		}

		res := Omit(s, "role") 

		// role should be omitted from the flattened result, but remain inside agent_user
		expected := map[string]any{
			"id": 1,
			"agent_user": &BaseUser{ID: 99, Role: "agent"},
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("omit from map", func(t *testing.T) {
		m := map[string]any{
			"a": 1,
			"b": "bee",
			"c": true,
		}
		res := Omit(m, "a", "c")

		expected := map[string]any{
			"b": "bee",
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("omit with pointer", func(t *testing.T) {
		s := &TestStruct{ID: 1, Name: "User", Email: "user@example.com"}
		res := Omit(s, "email")

		expected := map[string]any{
			"id":   1,
			"name": "User",
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("omit nil", func(t *testing.T) {
		res := Omit(nil, "any")
		if len(res) != 0 {
			t.Errorf("expected empty map, got %v", res)
		}
	})
}

func TestPick(t *testing.T) {
	type TestStruct struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Admin bool   `json:"-"`
	}

	t.Run("pick from struct", func(t *testing.T) {
		s := TestStruct{ID: 1, Name: "User", Email: "user@example.com", Admin: true}
		res := Pick(s, "name", "id")

		expected := map[string]any{
			"id":   1,
			"name": "User",
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("pick from embedded struct (flattened)", func(t *testing.T) {
		type Embedded struct {
			ID int `json:"id"`
		}
		type TestWithEmbedded struct {
			Embedded
			Name string `json:"name"`
		}

		s := TestWithEmbedded{
			Embedded: Embedded{ID: 100},
			Name:     "Tester",
		}
		res := Pick(s, "id")

		expected := map[string]any{
			"id": 100,
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("pick from map", func(t *testing.T) {
		m := map[string]any{
			"a": 1,
			"b": "bee",
			"c": true,
		}
		res := Pick(m, "a", "c")

		expected := map[string]any{
			"a": 1,
			"c": true,
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("pick with pointer", func(t *testing.T) {
		s := &TestStruct{ID: 1, Name: "User", Email: "user@example.com"}
		res := Pick(s, "email")

		expected := map[string]any{
			"email": "user@example.com",
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})

	t.Run("pick nil", func(t *testing.T) {
		res := Pick(nil, "any")
		if len(res) != 0 {
			t.Errorf("expected empty map, got %v", res)
		}
	})
}

func TestOmitSlice(t *testing.T) {
	type TestStruct struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	t.Run("omit from slice of structs", func(t *testing.T) {
		s := []TestStruct{
			{ID: 1, Name: "User1"},
			{ID: 2, Name: "User2"},
		}
		res := OmitSlice(s, "id")

		expected := []map[string]any{
			{"name": "User1"},
			{"name": "User2"},
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})
}

func TestPickSlice(t *testing.T) {
	type TestStruct struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	t.Run("pick from slice of structs", func(t *testing.T) {
		s := []TestStruct{
			{ID: 1, Name: "User1"},
			{ID: 2, Name: "User2"},
		}
		res := PickSlice(s, "name")

		expected := []map[string]any{
			{"name": "User1"},
			{"name": "User2"},
		}

		if !reflect.DeepEqual(res, expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}
	})
}
