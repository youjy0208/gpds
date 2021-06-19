// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package glog

import (
	"encoding/base64"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap/zapcore"

	"go.uber.org/zap/buffer"
)

var (
	_pool = buffer.NewPool()
	// Get retrieves a buffer from the pool, creating one if necessary.
	Get = _pool.Get
)

func addFields(enc zapcore.ObjectEncoder, fields []zapcore.Field) {
	for i := range fields {
		fields[i].AddTo(enc)
	}
}

// For Console-escaping; see consoleEncoder.safeAddString below.
const _hex = "0123456789abcdef"

var _consolePool = sync.Pool{New: func() interface{} {
	return &consoleEncoder{}
}}

func getConsoleEncoder() *consoleEncoder {
	return _consolePool.Get().(*consoleEncoder)
}

func putConsoleEncoder(enc *consoleEncoder) {
	if enc.reflectBuf != nil {
		enc.reflectBuf.Free()
	}
	enc.EncoderConfig = nil
	enc.buf = nil
	enc.spaced = false
	enc.openNamespaces = 0
	enc.reflectBuf = nil
	//enc.reflectEnc = nil
	_consolePool.Put(enc)
}

type consoleEncoder struct {
	*zapcore.EncoderConfig
	buf            *buffer.Buffer
	spaced         bool // include spaces after colons and commas
	openNamespaces int

	// for encoding generic values by reflection
	reflectBuf *buffer.Buffer
	//reflectEnc *json.Encoder
}

// NewConsoleEncoder creates a fast, low-allocation Console encoder. The encoder
// appropriately escapes all field keys and values.
//
// Note that the encoder doesn't deduplicate keys, so it's possible to produce
// a message like
//   {"foo":"bar","foo":"baz"}
// This is permitted by the Console specification, but not encouraged. Many
// libraries will ignore duplicate key-value pairs (typically keeping the last
// pair) when unmarshaling, but users should attempt to avoid adding duplicate
// keys.
func NewMyConsoleEncoder(cfg zapcore.EncoderConfig) zapcore.Encoder {
	return newConsoleEncoder(cfg, false)
}

func newConsoleEncoder(cfg zapcore.EncoderConfig, spaced bool) *consoleEncoder {
	return &consoleEncoder{
		EncoderConfig: &cfg,
		buf:           Get(),
		spaced:        spaced,
	}
}

func (enc *consoleEncoder) AddArray(key string, arr zapcore.ArrayMarshaler) error {
	return enc.AppendArray(arr)
}

func (enc *consoleEncoder) AddObject(key string, obj zapcore.ObjectMarshaler) error {
	return enc.AppendObject(obj)
}

func (enc *consoleEncoder) AddBinary(key string, val []byte) {
	enc.AddString(key, base64.StdEncoding.EncodeToString(val))
}

func (enc *consoleEncoder) AddByteString(key string, val []byte) {
	enc.AppendByteString(val)
}

func (enc *consoleEncoder) AddBool(key string, val bool) {
	enc.AppendBool(val)
}

func (enc *consoleEncoder) AddComplex128(key string, val complex128) {
	enc.AppendComplex128(val)
}

func (enc *consoleEncoder) AddDuration(key string, val time.Duration) {
	enc.AppendDuration(val)
}

func (enc *consoleEncoder) AddFloat64(key string, val float64) {
	enc.AppendFloat64(val)
}

func (enc *consoleEncoder) AddInt64(key string, val int64) {
	enc.AppendInt64(val)
}

func (enc *consoleEncoder) resetReflectBuf() {
	if enc.reflectBuf == nil {
		enc.reflectBuf = Get()
		//enc.reflectEnc = console.NewEncoder(enc.reflectBuf)

		// For consistency with our custom Console encoder.
		//enc.reflectEnc.SetEscapeHTML(false)
	} else {
		enc.reflectBuf.Reset()
	}
}

var nullLiteralBytes = []byte("null")

// Only invoke the standard Console encoder if there is actually something to
// encode; otherwise write Console null literal directly.
func (enc *consoleEncoder) encodeReflected(obj interface{}) ([]byte, error) {
	if obj == nil {
		return nullLiteralBytes, nil
	}
	enc.resetReflectBuf()
	//if err := enc.reflectEnc.Encode(obj); err != nil {
	//	return nil, err
	//}
	enc.reflectBuf.TrimNewline()
	return enc.reflectBuf.Bytes(), nil
}

func (enc *consoleEncoder) AddReflected(key string, obj interface{}) error {
	valueBytes, err := enc.encodeReflected(obj)
	if err != nil {
		return err
	}
	_, err = enc.buf.Write(valueBytes)
	return err
}

func (enc *consoleEncoder) OpenNamespace(key string) {
	enc.openNamespaces++
}

func (enc *consoleEncoder) AddString(key, val string) {
	enc.AppendString(val)
}

func (enc *consoleEncoder) AddTime(key string, val time.Time) {
	enc.AppendTime(val)
}

func (enc *consoleEncoder) AddUint64(key string, val uint64) {
	enc.AppendUint64(val)
}

func (enc *consoleEncoder) AppendArray(arr zapcore.ArrayMarshaler) error {
	enc.addElementSeparator()
	enc.buf.AppendByte('[')
	err := arr.MarshalLogArray(enc)
	enc.buf.AppendByte(']')
	return err
}

func (enc *consoleEncoder) AppendObject(obj zapcore.ObjectMarshaler) error {
	enc.addElementSeparator()
	enc.buf.AppendByte('{')
	err := obj.MarshalLogObject(enc)
	enc.buf.AppendByte('}')
	return err
}

func (enc *consoleEncoder) AppendBool(val bool) {
	enc.addElementSeparator()
	enc.buf.AppendBool(val)
}

func (enc *consoleEncoder) AppendByteString(val []byte) {
	enc.addElementSeparator()
	enc.buf.AppendByte('"')
	enc.safeAddByteString(val)
	enc.buf.AppendByte('"')
}

func (enc *consoleEncoder) AppendComplex128(val complex128) {
	enc.addElementSeparator()
	// Cast to a platform-independent, fixed-size type.
	r, i := float64(real(val)), float64(imag(val))
	enc.buf.AppendByte('"')
	// Because we're always in a quoted string, we can use strconv without
	// special-casing NaN and +/-Inf.
	enc.buf.AppendFloat(r, 64)
	enc.buf.AppendByte('+')
	enc.buf.AppendFloat(i, 64)
	enc.buf.AppendByte('i')
	enc.buf.AppendByte('"')
}

func (enc *consoleEncoder) AppendDuration(val time.Duration) {
	cur := enc.buf.Len()
	enc.EncodeDuration(val, enc)
	if cur == enc.buf.Len() {
		// User-supplied EncodeDuration is a no-op. Fall back to nanoseconds to keep
		// Console valid.
		enc.AppendInt64(int64(val))
	}
}

func (enc *consoleEncoder) AppendInt64(val int64) {
	enc.addElementSeparator()
	enc.buf.AppendInt(val)
}

func (enc *consoleEncoder) AppendReflected(val interface{}) error {
	valueBytes, err := enc.encodeReflected(val)
	if err != nil {
		return err
	}
	enc.addElementSeparator()
	_, err = enc.buf.Write(valueBytes)
	return err
}

func (enc *consoleEncoder) AppendString(val string) {
	enc.safeAddString(val)
}

func (enc *consoleEncoder) AppendTime(val time.Time) {
	cur := enc.buf.Len()
	enc.EncodeTime(val, enc)
	if cur == enc.buf.Len() {
		// User-supplied EncodeTime is a no-op. Fall back to nanos since epoch to keep
		// output Console valid.
		enc.AppendInt64(val.UnixNano())
	}
}

func (enc *consoleEncoder) AppendUint64(val uint64) {
	enc.addElementSeparator()
	enc.buf.AppendUint(val)
}

func (enc *consoleEncoder) AddComplex64(k string, v complex64) { enc.AddComplex128(k, complex128(v)) }
func (enc *consoleEncoder) AddFloat32(k string, v float32)     { enc.AddFloat64(k, float64(v)) }
func (enc *consoleEncoder) AddInt(k string, v int)             { enc.AddInt64(k, int64(v)) }
func (enc *consoleEncoder) AddInt32(k string, v int32)         { enc.AddInt64(k, int64(v)) }
func (enc *consoleEncoder) AddInt16(k string, v int16)         { enc.AddInt64(k, int64(v)) }
func (enc *consoleEncoder) AddInt8(k string, v int8)           { enc.AddInt64(k, int64(v)) }
func (enc *consoleEncoder) AddUint(k string, v uint)           { enc.AddUint64(k, uint64(v)) }
func (enc *consoleEncoder) AddUint32(k string, v uint32)       { enc.AddUint64(k, uint64(v)) }
func (enc *consoleEncoder) AddUint16(k string, v uint16)       { enc.AddUint64(k, uint64(v)) }
func (enc *consoleEncoder) AddUint8(k string, v uint8)         { enc.AddUint64(k, uint64(v)) }
func (enc *consoleEncoder) AddUintptr(k string, v uintptr)     { enc.AddUint64(k, uint64(v)) }
func (enc *consoleEncoder) AppendComplex64(v complex64)        { enc.AppendComplex128(complex128(v)) }
func (enc *consoleEncoder) AppendFloat64(v float64)            { enc.appendFloat(v, 64) }
func (enc *consoleEncoder) AppendFloat32(v float32)            { enc.appendFloat(float64(v), 32) }
func (enc *consoleEncoder) AppendInt(v int)                    { enc.AppendInt64(int64(v)) }
func (enc *consoleEncoder) AppendInt32(v int32)                { enc.AppendInt64(int64(v)) }
func (enc *consoleEncoder) AppendInt16(v int16)                { enc.AppendInt64(int64(v)) }
func (enc *consoleEncoder) AppendInt8(v int8)                  { enc.AppendInt64(int64(v)) }
func (enc *consoleEncoder) AppendUint(v uint)                  { enc.AppendUint64(uint64(v)) }
func (enc *consoleEncoder) AppendUint32(v uint32)              { enc.AppendUint64(uint64(v)) }
func (enc *consoleEncoder) AppendUint16(v uint16)              { enc.AppendUint64(uint64(v)) }
func (enc *consoleEncoder) AppendUint8(v uint8)                { enc.AppendUint64(uint64(v)) }
func (enc *consoleEncoder) AppendUintptr(v uintptr)            { enc.AppendUint64(uint64(v)) }

func (enc *consoleEncoder) Clone() zapcore.Encoder {
	clone := enc.clone()
	clone.buf.Write(enc.buf.Bytes())
	return clone
}

func (enc *consoleEncoder) clone() *consoleEncoder {
	clone := getConsoleEncoder()
	clone.EncoderConfig = enc.EncoderConfig
	clone.spaced = enc.spaced
	clone.openNamespaces = enc.openNamespaces
	clone.buf = Get()
	return clone
}

func (enc *consoleEncoder) truncate() {
	enc.buf.Reset()
}

func (enc *consoleEncoder) closeOpenNamespaces() {
	for i := 0; i < enc.openNamespaces; i++ {
		enc.buf.AppendByte('}')
	}
}

func (enc *consoleEncoder) addElementSeparator() {
	last := enc.buf.Len() - 1
	if last < 0 {
		return
	}
	switch enc.buf.Bytes()[last] {
	case '{', '[', ':', ',', ' ':
		return
	default:
		enc.buf.AppendByte(',')
		if enc.spaced {
			enc.buf.AppendByte(' ')
		}
	}
}

func (enc *consoleEncoder) appendFloat(val float64, bitSize int) {
	enc.addElementSeparator()
	switch {
	case math.IsNaN(val):
		enc.buf.AppendString(`"NaN"`)
	case math.IsInf(val, 1):
		enc.buf.AppendString(`"+Inf"`)
	case math.IsInf(val, -1):
		enc.buf.AppendString(`"-Inf"`)
	default:
		enc.buf.AppendFloat(val, bitSize)
	}
}

// safeAddString Console-escapes a string and appends it to the internal buffer.
// Unlike the standard library's encoder, it doesn't attempt to protect the
// user from browser vulnerabilities or ConsoleP-related problems.
func (enc *consoleEncoder) safeAddString(s string) {
	for i := 0; i < len(s); {
		if enc.tryAddRuneSelf(s[i]) {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if enc.tryAddRuneError(r, size) {
			i++
			continue
		}
		enc.buf.AppendString(s[i : i+size])
		i += size
	}
}

// safeAddByteString is no-alloc equivalent of safeAddString(string(s)) for s []byte.
func (enc *consoleEncoder) safeAddByteString(s []byte) {
	for i := 0; i < len(s); {
		if enc.tryAddRuneSelf(s[i]) {
			i++
			continue
		}
		r, size := utf8.DecodeRune(s[i:])
		if enc.tryAddRuneError(r, size) {
			i++
			continue
		}
		_, _ = enc.buf.Write(s[i : i+size])
		i += size
	}
}

// tryAddRuneSelf appends b if it is valid UTF-8 character represented in a single byte.
func (enc *consoleEncoder) tryAddRuneSelf(b byte) bool {
	if b >= utf8.RuneSelf {
		return false
	}
	if 0x20 <= b && b != '\\' && b != '"' {
		enc.buf.AppendByte(b)
		return true
	}
	switch b {
	case '\\', '"':
		enc.buf.AppendByte(b)
		// enc.buf.AppendByte('\\')
		// enc.buf.AppendByte(b)
	case '\n':
		enc.buf.AppendByte(b)
		// enc.buf.AppendByte('\\')
		// enc.buf.AppendByte('n')
	case '\r':
		enc.buf.AppendByte(b)
		// enc.buf.AppendByte('\\')
		// enc.buf.AppendByte('r')
	case '\t':
		enc.buf.AppendByte(b)
		// enc.buf.AppendByte('\\')
		// enc.buf.AppendByte('t')
	default:
		// Encode bytes < 0x20, except for the escape sequences above.
		enc.buf.AppendString(`\u00`)
		enc.buf.AppendByte(_hex[b>>4])
		enc.buf.AppendByte(_hex[b&0xF])
	}
	return true
}

func (enc *consoleEncoder) tryAddRuneError(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		enc.buf.AppendString(`\ufffd`)
		return true
	}
	return false
}

func (enc *consoleEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := enc.clone()

	final.AppendString("[")
	final.AppendTime(ent.Time)
	final.AppendString(" ")
	final.EncodeLevel(ent.Level, final)
	//final.AppendString(ent.Level,final)
	final.AppendString(" ")
	final.EncodeCaller(ent.Caller, final)
	//final.AppendString(ent.Caller.String())
	final.AppendString("] ")
	final.AppendString(ent.Message)
	if enc.buf.Len() > 0 {
		_, _ = final.buf.Write(enc.buf.Bytes())
	}
	addFields(final, fields)
	final.buf.AppendString(zapcore.DefaultLineEnding)
	ret := final.buf
	putConsoleEncoder(final)
	return ret, nil
}
