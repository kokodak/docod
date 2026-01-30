// sample.go
package main

import "fmt"

// User represents a user with a name and age.
type User struct {
	// Name is the user's full name.
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// Greeter is an interface for greeting.
type Greeter interface {
	Greet(name string) string
}


// MyFunction is a sample function.
// It prints a message.
func MyFunction(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

// MyMethod is a method on User.
func (u *User) MyMethod() {
	fmt.Println("This is a method.")
}
