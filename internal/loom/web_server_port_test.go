package loom

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestWebOccupiedPortDoesNotTakeOverListener(t *testing.T) {
	testHome(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

	err = cmdWeb([]string{port})
	if err == nil || !strings.Contains(err.Error(), "choisis un autre port") {
		t.Fatalf("expected an actionable occupied-port error, got %v", err)
	}
	probe, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("existing listener was disrupted: %v", err)
	}
	probe.Close()
}
