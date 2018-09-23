package main

// Assert panics if cond is false.
func Assert(cond bool) {
	if !cond {
		panic("Assertion failure")
	}
}
