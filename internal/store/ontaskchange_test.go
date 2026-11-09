package store

import "testing"

func TestOnTaskChangeFansOutAndUnsubscribes(t *testing.T) {
	var a, b []int64
	ua := OnTaskChange(func(id int64) { a = append(a, id) })
	ub := OnTaskChange(func(id int64) { b = append(b, id) })
	defer ub()
	notifyTask(1)
	ua()
	notifyTask(2)
	if len(a) != 1 || a[0] != 1 || len(b) != 2 {
		t.Fatalf("a=%v b=%v", a, b)
	}
}
