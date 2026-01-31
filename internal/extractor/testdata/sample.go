package sample

import "fmt"

// Version is the application version.
const Version = "1.0.0"

const (
	// StatusOK indicates success.
	StatusOK = 200
	// StatusError indicates failure.
	StatusError = 500
)

// GlobalVar is a global variable.
var GlobalVar = "hello"

// Base is a base struct.
type Base struct {
	ID int
}

// User is a complex struct.
type User struct {
	Base
	Name, Nickname string `json:"name"`
	Age            int    `json:"age"`
}

// Handler is an interface.
type Handler interface {
	fmt.Stringer
	Handle(ctx string, data interface{}) (int, error)
	Close()
}

// MyFunc is a function.
func MyFunc(a int, b string) bool {
	// Calling other function
	MyFunction("test")
	return true
}

// MyFunction is another function.
func MyFunction(s string) {}

// MyMethod is a method.
func (u *User) MyMethod(msg string) {
	fmt.Println(msg)
	// Instantiating another struct
	_ = Base{ID: 1}
	// Calling a built-in (should be ignored)
	_ = make([]int, 0)
}
