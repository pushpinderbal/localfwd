package discovery

import (
	"reflect"
	"testing"
)

func TestParseSS(t *testing.T) {
	input := `LISTEN 0 4096 127.0.0.1:3000 0.0.0.0:* users:(("node",pid=42,fd=20))
LISTEN 0 128 [::]:8080 [::]:* users:(("python",pid=9,fd=3))
LISTEN 0 128 *:3000 *:*
`
	want := []Listener{
		{Port: 3000, Address: "127.0.0.1", Process: "node"},
		{Port: 8080, Address: "::", Process: "python"},
	}
	got, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseNetstat(t *testing.T) {
	input := `Active Internet connections (only servers)
Proto Recv-Q Send-Q Local Address Foreign Address State PID/Program name
tcp 0 0 127.0.0.1:5432 0.0.0.0:* LISTEN 123/postgres
tcp6 0 0 :::9000 :::* LISTEN -
`
	got, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Port != 5432 || got[1].Port != 9000 {
		t.Fatalf("unexpected listeners: %#v", got)
	}
}

func TestParseLSOF(t *testing.T) {
	input := `COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
node 123 dev 20u IPv4 1 0t0 TCP 127.0.0.1:5173 (LISTEN)
`
	got, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{{Port: 5173, Address: "127.0.0.1", Process: "node"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}
