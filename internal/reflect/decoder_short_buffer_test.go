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

package reflect

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	stdreflect "reflect"
	"strings"
	"testing"

	"github.com/cloudwego/gopkg/protocol/thrift"
)

type shortBufferInner struct {
	A int32  `frugal:"1,default,i32"`
	B string `frugal:"2,default,string"`
}

type shortBufferStruct struct {
	Bool       bool                         `frugal:"1,default,bool"`
	Byte       int8                         `frugal:"2,default,i8"`
	I16        int16                        `frugal:"3,default,i16"`
	I32        int32                        `frugal:"4,default,i32"`
	I64        int64                        `frugal:"5,default,i64"`
	Double     float64                      `frugal:"6,default,double"`
	String     string                       `frugal:"7,default,string"`
	Binary     []byte                       `frugal:"8,default,binary"`
	ListI32    []int32                      `frugal:"9,default,list<i32>"`
	ListString []string                     `frugal:"10,default,list<string>"`
	Map        map[int32]string             `frugal:"11,default,map<i32:string>"`
	Struct     *shortBufferInner            `frugal:"12,default,shortBufferInner"`
	ListStruct []*shortBufferInner          `frugal:"13,default,list<shortBufferInner>"`
	NestedList [][]int32                    `frugal:"14,default,list<list<i32>>"`
	NestedMap  map[int32][]string           `frugal:"15,default,map<i32:list<string>>"`
	MapStruct  map[string]*shortBufferInner `frugal:"16,default,map<string:shortBufferInner>"`
}

func decodeWithPanicCapture(b []byte, v interface{}) (n int, panicValue interface{}, err error) {
	defer func() {
		panicValue = recover()
	}()
	n, err = Decode(b, v)
	return
}

func TestDecodeTruncatedInputReturnsError(t *testing.T) {
	src := &shortBufferStruct{
		Bool: true, Byte: -3, I16: 1234, I32: -99, I64: 1 << 40, Double: 3.5,
		String: "hello", Binary: []byte{1, 2, 3},
		ListI32: []int32{1, 2, 3}, ListString: []string{"a", "bb"},
		Map:        map[int32]string{7: "x"},
		Struct:     &shortBufferInner{A: 5, B: "in"},
		ListStruct: []*shortBufferInner{{A: 1}, {B: "z"}},
		NestedList: [][]int32{{1}, {2, 3}},
		NestedMap:  map[int32][]string{4: {"p", "q"}},
		MapStruct:  map[string]*shortBufferInner{"key": {A: 8, B: "value"}},
	}
	full, err := Append(nil, src)
	if err != nil {
		t.Fatal(err)
	}

	var complete shortBufferStruct
	if n, err := Decode(full, &complete); err != nil || n != len(full) || !stdreflect.DeepEqual(&complete, src) {
		t.Fatalf("full decode: n=%d/%d err=%v equal=%v", n, len(full), err, stdreflect.DeepEqual(&complete, src))
	}
	if n, err := thrift.Binary.Skip(full, thrift.STRUCT); err != nil || n != len(full) {
		t.Fatalf("reference rejected full fixture: n=%d err=%v", n, err)
	}
	for cut := 0; cut < len(full); cut++ {
		t.Run(fmt.Sprintf("cut_%d", cut), func(t *testing.T) {
			if _, err := thrift.Binary.Skip(full[:cut], thrift.STRUCT); err == nil {
				t.Fatal("reference accepted truncated fixture")
			}
			n, panicValue, err := decodeWithPanicCapture(full[:cut], &shortBufferStruct{})
			if panicValue != nil {
				t.Fatalf("Decode panicked on %d-byte prefix: %v", cut, panicValue)
			}
			if err == nil {
				t.Fatalf("Decode accepted truncated input: consumed %d of %d bytes", n, cut)
			}
			if n > cut {
				t.Fatalf("Decode reported consuming %d bytes from a %d-byte input", n, cut)
			}
		})
	}
}

func TestDecodeShortFieldHeadersAndScalars(t *testing.T) {
	for _, b := range [][]byte{nil, {byte(tI32)}, {byte(tI32), 0}} {
		n, err := Decode(b, &TestTypesOptional{})
		if !errors.Is(err, io.ErrShortBuffer) || n < 0 || n > len(b) {
			t.Fatalf("header %x: n=%d err=%v", b, n, err)
		}
	}
	for _, tc := range []struct {
		tp   ttype
		id   byte
		size int
	}{{tBOOL, 1, 1}, {tBYTE, 2, 1}, {tI16, 4, 2}, {tI32, 5, 4}, {tI64, 6, 8}, {tDOUBLE, 7, 8}, {tI32, 10, 4}} {
		for size := 0; size < tc.size; size++ {
			b := append([]byte{byte(tc.tp), 0, tc.id}, make([]byte, size)...)
			n, err := Decode(b, &TestTypesOptional{})
			if !errors.Is(err, io.ErrShortBuffer) || n < 0 || n > len(b) {
				t.Fatalf("type=%d id=%d size=%d: n=%d err=%v", tc.tp, tc.id, size, n, err)
			}
		}
	}
}

func TestDecodeShortFixedSizeMapEntries(t *testing.T) {
	// Variable-width entries can consume bytes counted by the minimum-size
	// precheck, leaving too few bytes for a later fixed-width key or value.
	type keyMap struct {
		M map[int32]string `frugal:"1,default,map<i32:string>"`
	}
	type valueMap struct {
		M map[string]int64 `frugal:"1,default,map<string:i64>"`
	}
	key := []byte{byte(tMAP), 0, 1, byte(tI32), byte(tSTRING), 0, 0, 0, 2,
		0, 0, 0, 1, 0, 0, 0, 20}
	key = append(key, []byte(strings.Repeat("x", 20))...)
	value := []byte{byte(tMAP), 0, 1, byte(tSTRING), byte(tI64), 0, 0, 0, 1, 0, 0, 0, 20}
	value = append(value, []byte(strings.Repeat("x", 20))...)
	for _, tc := range []struct {
		prefix []byte
		width  int
		dest   func() interface{}
	}{
		{key, 4, func() interface{} { return &keyMap{} }},
		{value, 8, func() interface{} { return &valueMap{} }},
	} {
		for size := 0; size < tc.width; size++ {
			b := append(append([]byte(nil), tc.prefix...), make([]byte, size)...)
			if _, err := thrift.Binary.Skip(b, thrift.STRUCT); err == nil {
				t.Fatal("reference accepted incomplete map")
			}
			n, err := Decode(b, tc.dest())
			if !errors.Is(err, io.ErrShortBuffer) || n < 0 || n > len(b) {
				t.Fatalf("width=%d size=%d: n=%d err=%v", tc.width, size, n, err)
			}
		}
	}
}

func TestDecodeRandomTruncations(t *testing.T) {
	r := rand.New(rand.NewSource(20261014))
	checks := 0
	for trial := 0; trial < 128; trial++ {
		src := &shortBufferStruct{
			Bool: r.Intn(2) == 1, Byte: int8(r.Int()), I16: int16(r.Int()),
			I32: r.Int31(), I64: r.Int63(), Double: r.Float64(),
			String: strings.Repeat("a", r.Intn(40)), Binary: make([]byte, r.Intn(40)),
			ListI32: make([]int32, r.Intn(10)), ListString: []string{"", strings.Repeat("b", r.Intn(30))},
			Map:        map[int32]string{r.Int31(): strings.Repeat("c", r.Intn(30))},
			Struct:     &shortBufferInner{A: r.Int31(), B: "inner"},
			ListStruct: []*shortBufferInner{{A: r.Int31()}, {B: "list"}},
			NestedList: [][]int32{{}, {r.Int31(), r.Int31()}},
			NestedMap:  map[int32][]string{r.Int31(): {"", "nested"}},
			MapStruct:  map[string]*shortBufferInner{"key": {A: r.Int31(), B: "value"}},
		}
		full, err := Append(nil, src)
		if err != nil {
			t.Fatal(err)
		}
		var dst shortBufferStruct
		if n, err := Decode(full, &dst); err != nil || n != len(full) || !stdreflect.DeepEqual(src, &dst) {
			t.Fatalf("trial=%d full decode mismatch n=%d err=%v", trial, n, err)
		}
		if n, err := thrift.Binary.Skip(full, thrift.STRUCT); err != nil || n != len(full) {
			t.Fatalf("reference rejected full fixture: n=%d err=%v", n, err)
		}
		for cut := 0; cut < len(full); cut++ {
			if _, err := thrift.Binary.Skip(full[:cut], thrift.STRUCT); err == nil {
				t.Fatalf("reference trial=%d cut=%d", trial, cut)
			}
			n, p, err := decodeWithPanicCapture(full[:cut], &shortBufferStruct{})
			if p != nil || err == nil || n < 0 || n > cut {
				t.Fatalf("trial=%d cut=%d n=%d err=%v panic=%v", trial, cut, n, err, p)
			}
			checks++
		}
	}
	t.Logf("verified %d truncated inputs and 128 complete round trips", checks)
}

func TestDecodeUnknownFieldsAndTypeMismatchTruncations(t *testing.T) {
	type wireStruct struct {
		Unknown [][]int32 `frugal:"10,default,list<list<i32>>"`
		Value   int32     `frugal:"1,default,i32"`
	}
	type destStruct struct {
		Value string `frugal:"1,default,string"`
	}
	full, err := Append(nil, &wireStruct{Unknown: [][]int32{{1, 2}, {3}}, Value: 42})
	if err != nil {
		t.Fatal(err)
	}
	var dst destStruct
	if n, err := Decode(full, &dst); err != nil || n != len(full) || dst.Value != "" {
		t.Fatalf("skip full message: n=%d err=%v value=%q", n, err, dst.Value)
	}
	for cut := 0; cut < len(full); cut++ {
		if _, err := thrift.Binary.Skip(full[:cut], thrift.STRUCT); err == nil {
			t.Fatalf("reference accepted prefix %d", cut)
		}
		n, p, err := decodeWithPanicCapture(full[:cut], &destStruct{})
		if p != nil || err == nil || n < 0 || n > cut {
			t.Fatalf("skip prefix=%d n=%d err=%v panic=%v", cut, n, err, p)
		}
	}
}
