package caller

import "reflect"

// services holds how each resource package makes its services, by the service's type. It is
// written only while the packages are initialised, and read after.
var services = map[reflect.Type]any{}

// Register records how a resource package makes its service S for a Caller. Each resource package
// registers its services when it is initialised, so package cru can make them with NewService
// while they export no constructor programs would see.
func Register[S any](newService func(*Caller) *S) {
	services[reflect.TypeFor[S]()] = newService
}

// NewService makes the service S for c, as its package registered it. It panics for a service no
// package registered, which the generated code rules out.
func NewService[S any](c *Caller) *S {
	newService, ok := services[reflect.TypeFor[S]()].(func(*Caller) *S)
	if !ok {
		panic("caller: no service " + reflect.TypeFor[S]().String() + " is registered")
	}
	return newService(c)
}
