package core

import "testing"

func TestClassLike(t *testing.T) {
	classlike := []Kind{KindClass, KindStruct, KindInterface, KindEnum}
	not := []Kind{KindModule, KindMethod, KindFunction, KindConstant, KindVariable}
	for _, k := range classlike {
		if !k.ClassLike() {
			t.Errorf("%s should be class-like", k)
		}
	}
	for _, k := range not {
		if k.ClassLike() {
			t.Errorf("%s should not be class-like", k)
		}
	}
}
