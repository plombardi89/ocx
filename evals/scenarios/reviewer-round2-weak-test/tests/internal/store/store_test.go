package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestPutValidates(t *testing.T) {
	s := New()
	bad := []Product{
		{Name: "no id", Price: 1},
		{ID: "a", Price: 1},
		{ID: "a", Name: "negative price", Price: -1},
		{ID: "a", Name: "negative stock", Price: 1, Stock: -1},
	}
	for _, p := range bad {
		if err := s.Put(p); !errors.Is(err, ErrInvalidProduct) {
			t.Errorf("Put(%+v) = %v, want ErrInvalidProduct", p, err)
		}
	}
	if len(s.All()) != 0 {
		t.Fatal("invalid products were stored")
	}
}

func TestPutGetAll(t *testing.T) {
	s := New()
	for _, id := range []string{"c", "a", "b"} {
		if err := s.Put(Product{ID: id, Name: "item " + id, Price: 100, Stock: 1}); err != nil {
			t.Fatal(err)
		}
	}
	p, ok := s.Get("b")
	if !ok || p.Name != "item b" {
		t.Fatalf("Get(b) = %+v, %v", p, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("Get(missing) found a product")
	}
	all := s.All()
	if len(all) != 3 || all[0].ID != "a" || all[1].ID != "b" || all[2].ID != "c" {
		t.Fatalf("All() = %+v, want a, b, c", all)
	}
}

func TestConcurrentPutAndGet(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.Put(Product{ID: fmt.Sprint(i), Name: "x", Price: 1})
		}()
		go func() {
			defer wg.Done()
			s.Get(fmt.Sprint(i))
			s.All()
		}()
	}
	wg.Wait()
	if n := len(s.All()); n != 50 {
		t.Fatalf("stored %d products, want 50", n)
	}
}

func newListStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	for _, id := range []string{"e", "d", "c", "b", "a"} {
		if err := s.Put(Product{ID: id, Name: id, Price: 1}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestList(t *testing.T) {
	s := newListStore(t)
	page, total := s.List(0, 2)
	if total != 5 || len(page) != 2 || page[0].ID != "a" || page[1].ID != "b" {
		t.Fatalf("List(0, 2) = %+v, %d", page, total)
	}
	page, total = s.List(2, 2)
	if total != 5 || len(page) != 2 || page[0].ID != "c" || page[1].ID != "d" {
		t.Fatalf("List(2, 2) = %+v, %d", page, total)
	}
	page, _ = s.List(4, 2)
	if len(page) != 1 || page[0].ID != "e" {
		t.Fatalf("List(4, 2) = %+v", page)
	}
}

func TestListPastEnd(t *testing.T) {
	s := newListStore(t)
	page, total := s.List(5, 20)
	if len(page) != 0 || total != 5 {
		t.Errorf("List(5, 20) = %#v, %d; want an empty page of 5", page, total)
	}
}
