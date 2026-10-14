/*
 * Copyright 2026 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tests

import (
	"reflect"
	"testing"

	"github.com/apache/thrift/lib/go/thrift"
	"github.com/cloudwego/frugal"
	"github.com/cloudwego/frugal/tests/baseline"
)

func TestDecodeObjectTruncatedApacheThrift(t *testing.T) {
	simple := &baseline.Simple{ByteField: 1, I64Field: 1 << 40, DoubleField: 3.5,
		I32Field: -7, StringField: "nested", BinaryField: []byte{1, 2}, EnumField: baseline.Enums_ValueC}
	src := &baseline.Nesting{String_: "fixture", ListSimple: []*baseline.Simple{simple},
		Double: 2.5, I32: 42, ListI32: []int32{1, 2}, I64: 9,
		MapStringString: map[string]string{"k": "v"}, SimpleStruct: simple,
		MapI32I64: map[int32]int64{1: 2}, ListString: []string{"a", "b"},
		Binary: []byte{3}, MapI64String: map[int64]string{3: "value"},
		ListI64: []int64{4}, Byte: 2, MapStringSimple: map[string]*baseline.Simple{"inner": simple}}
	mm := thrift.NewTMemoryBuffer()
	if err := src.Write(thrift.NewTBinaryProtocolTransport(mm)); err != nil {
		t.Fatal(err)
	}
	wire := mm.Bytes()
	var complete baseline.Nesting
	if n, err := frugal.DecodeObject(wire, &complete); err != nil || n != len(wire) || !reflect.DeepEqual(src, &complete) {
		t.Fatalf("complete Apache fixture failed: n=%d err=%v", n, err)
	}
	for cut := 0; cut < len(wire); cut++ {
		input := wire[:cut]
		ref := thrift.NewTMemoryBuffer()
		if _, err := ref.Write(input); err != nil {
			t.Fatal(err)
		}
		var expected baseline.Nesting
		if err := expected.Read(thrift.NewTBinaryProtocolTransport(ref)); err == nil {
			t.Fatalf("Apache accepted truncated prefix %d", cut)
		}
		var dst baseline.Nesting
		n, err := frugal.DecodeObject(input, &dst)
		if err == nil || n < 0 || n > len(input) {
			t.Fatalf("prefix=%d: n=%d err=%v", cut, n, err)
		}
	}
	t.Logf("validated all %d prefixes of an Apache-generated nested message", len(wire))
}
