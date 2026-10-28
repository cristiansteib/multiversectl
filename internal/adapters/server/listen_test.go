package server

import (
	"net"
	"testing"
)

func TestListenNearSkipsABusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer busy.Close()
	ln, err := listenNear(busy.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ln.Addr().String() == busy.Addr().String() {
		t.Fatalf("listened on the busy address %s", ln.Addr())
	}
}
