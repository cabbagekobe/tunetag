package id3v2

import (
	"bytes"
	"errors"
	"testing"
)

func TestReadHeader_NoMagic(t *testing.T) {
	r := bytes.NewReader([]byte("not_an_id3_tag_at_all_____"))
	if _, err := readHeader(r); !errors.Is(err, ErrNoTag) {
		t.Fatalf("got %v, want ErrNoTag", err)
	}
}

func TestReadHeader_Empty(t *testing.T) {
	if _, err := readHeader(bytes.NewReader(nil)); !errors.Is(err, ErrNoTag) {
		t.Fatalf("got %v, want ErrNoTag", err)
	}
}

func TestReadHeader_UnsupportedVersion(t *testing.T) {
	b := []byte{'I', 'D', '3', 5, 0, 0, 0, 0, 0, 0}
	if _, err := readHeader(bytes.NewReader(b)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}

func TestReadHeader_MalformedSize(t *testing.T) {
	b := []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0} // top bit set in size
	if _, err := readHeader(bytes.NewReader(b)); err == nil {
		t.Fatalf("expected error for malformed synchsafe size")
	}
}

func TestHeader_RoundTrip(t *testing.T) {
	in := Header{Version: V23, Flags: FlagExperimental, Size: 12345}
	var buf bytes.Buffer
	if err := in.writeTo(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := readHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("got %+v, want %+v", out, in)
	}
}
