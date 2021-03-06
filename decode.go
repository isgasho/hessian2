/*
 *
 *  * Copyright 2012-2016 Viant.
 *  *
 *  * Licensed under the Apache License, Version 2.0 (the "License"); you may not
 *  * use this file except in compliance with the License. You may obtain a copy of
 *  * the License at
 *  *
 *  * http://www.apache.org/licenses/LICENSE-2.0
 *  *
 *  * Unless required by applicable law or agreed to in writing, software
 *  * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 *  * WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
 *  * License for the specific language governing permissions and limitations under
 *  * the License.
 *
 */

/*
decoder implement hessian 2 protocol, It follows java hessian package standard.
It assume that you using the java name convention
baisca difference between java and go
fully qualify java class name is composed of package + class name
Go assume upper case of field name is exportable and java did not have that constrain
but in general java using camo camlecase. So it did conversion of field name from
the first letter of from upper to lower case
typMap{string]reflect.Type contain full java package+class name and go relfect.Type
must provide in order to correctly decode to galang interface
*/

// Copyright (c) 2016 ~ 2019, Alex Stocks.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hessian

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
)

import (
	jerrors "github.com/juju/errors"
)

type Decoder struct {
	reader        *bufio.Reader
	refs          []interface{}
	classInfoList []classInfo
}

var (
	ErrNotEnoughBuf    = jerrors.Errorf("not enough buf")
	ErrIllegalRefIndex = jerrors.Errorf("illegal ref index")
)

func NewDecoder(b []byte) *Decoder {
	return &Decoder{reader: bufio.NewReader(bytes.NewReader(b))}
}

/////////////////////////////////////////
// utilities
/////////////////////////////////////////

// 读取当前字节,指针不前移
func (d *Decoder) peekByte() byte {
	return d.peek(1)[0]
}

// 获取缓冲长度
func (d *Decoder) len() int {
	d.peek(1) //需要先读一下资源才能得到已缓冲的长度
	return d.reader.Buffered()
}

// 读取 Decoder 结构中的一个字节,并后移一个字节
func (d *Decoder) readByte() (byte, error) {
	return d.reader.ReadByte()
}

// 前移一个字节
func (d *Decoder) unreadByte() error {
	return d.reader.UnreadByte()
}

// 读取指定长度的字节,并后移len(b)个字节
func (d *Decoder) next(b []byte) (int, error) {
	return d.reader.Read(b)
}

// 读取指定长度字节,指针不后移
func (d *Decoder) peek(n int) []byte {
	b, _ := d.reader.Peek(n)
	return b
}

// 读取len(s)的 utf8 字符
func (d *Decoder) nextRune(s []rune) []rune {
	var (
		n   int
		i   int
		r   rune
		ri  int
		err error
	)

	n = len(s)
	s = s[:0]
	for i = 0; i < n; i++ {
		if r, ri, err = d.reader.ReadRune(); err == nil && ri > 0 {
			s = append(s, r)
		}
	}

	return s
}

// 读取数据类型描述,用于 list 和 map
func (d *Decoder) decType() (string, error) {
	var (
		err error
		arr [1]byte
		buf []byte
		tag byte
		idx int32
		typ reflect.Type
	)

	buf = arr[:1]
	if _, err = io.ReadFull(d.reader, buf); err != nil {
		return "", jerrors.Trace(err)
	}
	tag = buf[0]
	if (tag >= BC_STRING_DIRECT && tag <= STRING_DIRECT_MAX) ||
		(tag >= 0x30 && tag <= 0x33) || (tag == BC_STRING) || (tag == BC_STRING_CHUNK) {
		return d.decString(int32(tag))
	}

	if idx, err = d.decInt32(TAG_READ); err != nil {
		return "", jerrors.Trace(err)
	}

	typ, _, err = d.getStructDefByIndex(int(idx))
	if err == nil {
		return typ.String(), nil
	}

	return "", err
}

// 解析 hessian 数据包
func (d *Decoder) Decode() (interface{}, error) {
	var (
		err error
		tag byte
	)

	tag, err = d.readByte()
	if err == io.EOF {
		return nil, err
	}

	switch {
	case tag == BC_END:
		// return EOF error for end flag 'Z'
		return nil, io.EOF

	case tag == BC_NULL: // 'N': //null
		return nil, nil

	case tag == BC_TRUE: // 'T': //true
		return true, nil

	case tag == BC_FALSE: //'F': //false
		return false, nil

	case tag == BC_REF: // 'R': //ref, 一个整数，用以指代前面的list 或者 map
		return d.decRef(int32(tag))

	case (0x80 <= tag && tag <= 0xbf) || (0xc0 <= tag && tag <= 0xcf) ||
		(0xd0 <= tag && tag <= 0xd7) || tag == BC_INT: //'I': //int
		return d.decInt32(int32(tag))

	case (tag >= 0xd8 && tag <= 0xef) || (tag >= 0xf0 && tag <= 0xff) ||
		(tag >= 0x38 && tag <= 0x3f) || (tag == BC_LONG_INT) || (tag == BC_LONG): //'L': //long
		return d.decInt64(int32(tag))

	case (tag == BC_DATE_MINUTE) || (tag == BC_DATE): //'d': //date
		return d.decDate(int32(tag))

	case (tag == BC_DOUBLE_ZERO) || (tag == BC_DOUBLE_ONE) || (tag == BC_DOUBLE_BYTE) ||
		(tag == BC_DOUBLE_SHORT) || (tag == BC_DOUBLE_MILL) || (tag == BC_DOUBLE): //'D': //double
		return d.decDouble(int32(tag))

	// case 'S', 's', 'X', 'x': //string,xml
	case (tag == BC_STRING_CHUNK || tag == BC_STRING) ||
		(tag >= BC_STRING_DIRECT && tag <= STRING_DIRECT_MAX) ||
		(tag >= 0x30 && tag <= 0x33):
		return d.decString(int32(tag))

		// case 'B', 'b': //binary
	case (tag == BC_BINARY) || (tag == BC_BINARY_CHUNK) || (tag >= 0x20 && tag <= 0x2f) ||
		(tag >= BC_BINARY_SHORT && tag <= 0x3f):
		return d.decBinary(int32(tag))

	// case 'V': //list
	case (tag >= BC_LIST_DIRECT && tag <= 0x77) || (tag == BC_LIST_FIXED || tag == BC_LIST_VARIABLE) ||
		(tag >= BC_LIST_DIRECT_UNTYPED && tag <= 0x7f) ||
		(tag == BC_LIST_FIXED_UNTYPED || tag == BC_LIST_VARIABLE_UNTYPED):
		return d.decList(int32(tag))

	case (tag == BC_MAP) || (tag == BC_MAP_UNTYPED):
		return d.decMap(int32(tag))

	case (tag == BC_OBJECT_DEF) || (tag == BC_OBJECT) ||
		(BC_OBJECT_DIRECT <= tag && tag <= (BC_OBJECT_DIRECT+OBJECT_DIRECT_MAX)):
		return d.decObject(int32(tag))

	default:
		return nil, jerrors.Errorf("Invalid type: %v,>>%v<<<", string(tag), d.peek(d.len()))
	}
}
