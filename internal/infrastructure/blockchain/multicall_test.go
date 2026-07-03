package blockchain

import (
	"reflect"
	"testing"
)

func TestDecodeAggregate3Results(t *testing.T) {
	raw := []struct {
		Success    bool   `json:"success"`
		ReturnData []byte `json:"returnData"`
	}{
		{Success: true, ReturnData: []byte{0x01, 0x02}},
		{Success: false, ReturnData: nil},
	}

	got, err := decodeAggregate3Results(raw)
	if err != nil {
		t.Fatalf("decode aggregate3 results: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if !got[0].Success || string(got[0].ReturnData) != "\x01\x02" {
		t.Fatalf("unexpected first result: %+v", got[0])
	}
	if got[1].Success || len(got[1].ReturnData) != 0 {
		t.Fatalf("unexpected second result: %+v", got[1])
	}
}

func TestDecodeAggregate3ResultsGeneratedStruct(t *testing.T) {
	generated := reflect.StructOf([]reflect.StructField{
		{Name: "Success", Type: reflect.TypeOf(false)},
		{Name: "ReturnData", Type: reflect.TypeOf([]byte(nil))},
	})
	sliceType := reflect.SliceOf(generated)
	raw := reflect.MakeSlice(sliceType, 1, 1)
	item := raw.Index(0)
	item.FieldByName("Success").SetBool(true)
	item.FieldByName("ReturnData").SetBytes([]byte{0xaa})

	got, err := decodeAggregate3Results(raw.Interface())
	if err != nil {
		t.Fatalf("decode generated aggregate3 results: %v", err)
	}
	if len(got) != 1 || !got[0].Success || got[0].ReturnData[0] != 0xaa {
		t.Fatalf("unexpected decoded result: %+v", got)
	}
}
