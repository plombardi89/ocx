package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestReserveEndpoint(t *testing.T) {
	srv, s := newServer(t)
	cases := []struct {
		id, body string
		want     int
	}{
		{"a", `{"quantity":3}`, http.StatusNoContent},
		{"a", `{"quantity":0}`, http.StatusBadRequest},
		{"a", `{"qty":1}`, http.StatusBadRequest},
		{"a", `nope`, http.StatusBadRequest},
		{"zzz", `{"quantity":1}`, http.StatusNotFound},
		{"a", `{"quantity":8}`, http.StatusConflict},
	}
	for _, c := range cases {
		resp, err := http.Post(srv.URL+"/products/"+c.id+"/reserve", "application/json", strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("reserve %s %s: status %d, want %d", c.id, c.body, resp.StatusCode, c.want)
		}
	}
	if p, _ := s.Get("a"); p.Stock != 7 {
		t.Fatalf("stock = %d, want 7", p.Stock)
	}
}
