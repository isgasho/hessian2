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
	"io"
	"reflect"
	"strings"
)

import (
	jerrors "github.com/juju/errors"
)

// get @v go struct name
func typeof(v interface{}) string {
	return reflect.TypeOf(v).String()
}

/////////////////////////////////////////
// map/object
/////////////////////////////////////////

//class-def  ::= 'C' string int string* //  mandatory type string, the number of fields, and the field names.
//object     ::= 'O' int value* // class-def id, value list
//           ::= [x60-x6f] value* // class-def id, value list
//
//Object serialization
//
//class Car {
//  String color;
//  String model;
//}
//
//out.writeObject(new Car("red", "corvette"));
//out.writeObject(new Car("green", "civic"));
//
//---
//
//C                        # object definition (#0)
//  x0b example.Car        # type is example.Car
//  x92                    # two fields
//  x05 color              # color field name
//  x05 model              # model field name
//
//O                        # object def (long form)
//  x90                    # object definition #0
//  x03 red                # color field value
//  x08 corvette           # model field value
//
//x60                      # object def #0 (short form)
//  x05 green              # color field value
//  x05 civic              # model field value
//
//enum Color {
//  RED,
//  GREEN,
//  BLUE,
//}
//
//out.writeObject(Color.RED);
//out.writeObject(Color.GREEN);
//out.writeObject(Color.BLUE);
//out.writeObject(Color.GREEN);
//
//---
//
//C                         # class definition #0
//  x0b example.Color       # type is example.Color
//  x91                     # one field
//  x04 name                # enumeration field is "name"
//
//x60                       # object #0 (class def #0)
//  x03 RED                 # RED value
//
//x60                       # object #1 (class def #0)
//  x90                     # object definition ref #0
//  x05 GREEN               # GREEN value
//
//x60                       # object #2 (class def #0)
//  x04 BLUE                # BLUE value
//
//x51 x91                   # object ref #1, i.e. Color.GREEN
func (e *Encoder) encObject(v POJO) error {
	var (
		ok     bool
		i      int
		idx    int
		num    int
		err    error
		clsDef classInfo
	)

	vv := reflect.ValueOf(v)
	// check ref
	if n, ok := e.checkRefMap(vv); ok {
		e.buffer = encRef(e.buffer, n)
		return nil
	}

	vv = UnpackPtr(vv)
	// check nil pointer
	if !vv.IsValid() {
		e.buffer = encNull(e.buffer)
		return nil
	}

	// write object definition
	idx = -1
	for i = range e.classInfoList {
		if v.JavaClassName() == e.classInfoList[i].javaName {
			idx = i
			break
		}
	}
	if idx == -1 {
		idx, ok = checkPOJORegistry(typeof(v))
		if !ok { // 不存在
			if reflect.TypeOf(v).Implements(javaEnumType) {
				idx = RegisterJavaEnum(v.(POJOEnum))
			} else {
				idx = RegisterPOJO(v)
			}
		}
		_, clsDef, _ = getStructDefByIndex(idx)
		idx = len(e.classInfoList)
		e.classInfoList = append(e.classInfoList, clsDef)
		e.buffer = append(e.buffer, clsDef.buffer...)
	}

	// write object instance
	if byte(idx) <= OBJECT_DIRECT_MAX {
		e.buffer = encByte(e.buffer, byte(idx)+BC_OBJECT_DIRECT)
	} else {
		e.buffer = encByte(e.buffer, BC_OBJECT)
		e.buffer = encInt32(e.buffer, int32(idx))
	}

	if reflect.TypeOf(v).Implements(javaEnumType) {
		e.buffer = encString(e.buffer, v.(POJOEnum).String())
		return nil
	}
	num = vv.NumField()
	for i = 0; i < num; i++ {
		field := vv.Field(i)
		fieldName := field.Type().String()
		if err = e.Encode(field.Interface()); err != nil {
			return jerrors.Annotatef(err, "failed to encode field: %s, %+v", fieldName, field.Interface())
		}
	}

	return nil
}

/////////////////////////////////////////
// Object
/////////////////////////////////////////

//class-def  ::= 'C' string int string* //  mandatory type string, the number of fields, and the field names.
//object     ::= 'O' int value* // class-def id, value list
//           ::= [x60-x6f] value* // class-def id, value list
//
//Object serialization
//
//class Car {
//  String color;
//  String model;
//}
//
//out.writeObject(new Car("red", "corvette"));
//out.writeObject(new Car("green", "civic"));
//
//---
//
//C                        # object definition (#0)
//  x0b example.Car        # type is example.Car
//  x92                    # two fields
//  x05 color              # color field name
//  x05 model              # model field name
//
//O                        # object def (long form)
//  x90                    # object definition #0
//  x03 red                # color field value
//  x08 corvette           # model field value
//
//x60                      # object def #0 (short form)
//  x05 green              # color field value
//  x05 civic              # model field value
//
//
//
//
//
//enum Color {
//  RED,
//  GREEN,
//  BLUE,
//}
//
//out.writeObject(Color.RED);
//out.writeObject(Color.GREEN);
//out.writeObject(Color.BLUE);
//out.writeObject(Color.GREEN);
//
//---
//
//C                         # class definition #0
//  x0b example.Color       # type is example.Color
//  x91                     # one field
//  x04 name                # enumeration field is "name"
//
//x60                       # object #0 (class def #0)
//  x03 RED                 # RED value
//
//x60                       # object #1 (class def #0)
//  x90                     # object definition ref #0
//  x05 GREEN               # GREEN value
//
//x60                       # object #2 (class def #0)
//  x04 BLUE                # BLUE value
//
//x51 x91                   # object ref #1, i.e. Color.GREEN

func (d *Decoder) decClassDef() (interface{}, error) {
	var (
		err       error
		clsName   string
		fieldNum  int32
		fieldName string
		fieldList []string
	)

	clsName, err = d.decString(TAG_READ)
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	fieldNum, err = d.decInt32(TAG_READ)
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	fieldList = make([]string, fieldNum)
	for i := 0; i < int(fieldNum); i++ {
		fieldName, err = d.decString(TAG_READ)
		if err != nil {
			return nil, jerrors.Annotatef(err, "decClassDef->decString, filed num:%d, index:%d", fieldNum, i)
		}
		fieldList[i] = fieldName
	}

	return classInfo{javaName: clsName, fieldNameList: fieldList}, nil
}

func findField(name string, typ reflect.Type) (int, error) {
	for i := 0; i < typ.NumField(); i++ {
		str := typ.Field(i).Name
		if strings.Compare(str, name) == 0 {
			return i, nil
		}
		// str1 := strings.Title(name)
		str1 := strings.ToLower(str)
		if strings.Compare(name, str1) == 0 {
			return i, nil
		}
	}

	return 0, jerrors.Errorf("failed to find field %s", name)
}

func (d *Decoder) decInstance(typ reflect.Type, cls classInfo) (interface{}, error) {
	if typ.Kind() != reflect.Struct {
		return nil, jerrors.Errorf("wrong type expect Struct but get:%s", typ.String())
	}

	vRef := reflect.New(typ)
	// add pointer ref so that ref the same object
	d.appendRefs(vRef)

	vv := vRef.Elem()
	for i := 0; i < len(cls.fieldNameList); i++ {
		fieldName := cls.fieldNameList[i]

		index, err := findField(fieldName, typ)
		if err != nil {
			return nil, jerrors.Errorf("can not find field %s", fieldName)
		}
		field := vv.Field(index)
		if !field.CanSet() {
			return nil, jerrors.Errorf("decInstance CanSet false for field %s", fieldName)
		}

		// get field type from type object, not do that from value
		fldTyp := UnpackPtrType(field.Type())

		// unpack pointer to enable value setting
		fldRawValue := UnpackPtrValue(field)

		kind := fldTyp.Kind()
		switch {
		case kind == reflect.String:
			str, err := d.decString(TAG_READ)
			if err != nil {
				return nil, jerrors.Annotatef(err, "decInstance->ReadString: %s", fieldName)
			}
			fldRawValue.SetString(str)

		case kind == reflect.Int32 || kind == reflect.Int16:
			num, err := d.decInt32(TAG_READ)
			if err != nil {
				// java enum
				if fldRawValue.Type().Implements(javaEnumType) {
					d.unreadByte() // enum解析，上面decInt64已经读取一个字节，所以这里需要回退一个字节
					s, err := d.Decode()
					if err != nil {
						return nil, jerrors.Annotatef(err, "decInstance->decObject field name:%s", fieldName)
					}
					enumValue, _ := s.(JavaEnum)
					num = int32(enumValue)
				} else {
					return nil, jerrors.Annotatef(err, "decInstance->ParseInt, field name:%s", fieldName)
				}
			}

			fldRawValue.SetInt(int64(num))

		case kind == reflect.Int || kind == reflect.Int64 || kind == reflect.Uint64:
			num, err := d.decInt64(TAG_READ)
			if err != nil {
				if fldTyp.Implements(javaEnumType) {
					d.unreadByte() // enum解析，上面decInt64已经读取一个字节，所以这里需要回退一个字节
					s, err := d.Decode()
					if err != nil {
						return nil, jerrors.Annotatef(err, "decInstance->decObject field name:%s", fieldName)
					}
					enumValue, _ := s.(JavaEnum)
					num = int64(enumValue)
				} else {
					return nil, jerrors.Annotatef(err, "decInstance->decInt64 field name:%s", fieldName)
				}
			}

			fldRawValue.SetInt(num)

		case kind == reflect.Bool:
			b, err := d.Decode()
			if err != nil {
				return nil, jerrors.Annotatef(err, "decInstance->Decode field name:%s", fieldName)
			}
			fldRawValue.SetBool(b.(bool))

		case kind == reflect.Float32 || kind == reflect.Float64:
			num, err := d.decDouble(TAG_READ)
			if err != nil {
				return nil, jerrors.Annotatef(err, "decInstance->decDouble field name:%s", fieldName)
			}
			fldRawValue.SetFloat(num.(float64))

		case kind == reflect.Map:
			// decode map should use the original field value for correct value setting
			err := d.decMapByValue(field)
			if err != nil {
				return nil, jerrors.Annotatef(err, "decInstance->decMapByValue field name: %s", fieldName)
			}

		case kind == reflect.Slice || kind == reflect.Array:
			m, err := d.decList(TAG_READ)
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, jerrors.Trace(err)
			}

			// set slice separately
			err = SetSlice(fldRawValue, m)
			if err != nil {
				return nil, err
			}
		case kind == reflect.Struct:
			var (
				err error
				s   interface{}
			)
			if fldRawValue.Type().String() == "time.Time" {
				s, err = d.decDate(TAG_READ)
				if err != nil {
					return nil, jerrors.Trace(err)
				}
				fldRawValue.Set(reflect.ValueOf(s))
			} else {
				s, err = d.decObject(TAG_READ)
				if err != nil {
					return nil, jerrors.Trace(err)
				}
				if s != nil {
					// set value which accepting pointers
					SetValue(fldRawValue, EnsurePackValue(s))
				}
			}

		default:
			return nil, jerrors.Errorf("unknown struct member type: %v", kind)
		}
	} // end for

	return vRef, nil
}

func (d *Decoder) appendClsDef(cd classInfo) {
	d.classInfoList = append(d.classInfoList, cd)
}

func (d *Decoder) getStructDefByIndex(idx int) (reflect.Type, classInfo, error) {
	var (
		ok      bool
		clsName string
		cls     classInfo
		s       structInfo
	)

	if len(d.classInfoList) <= idx || idx < 0 {
		return nil, cls, jerrors.Errorf("illegal class index @idx %d", idx)
	}
	cls = d.classInfoList[idx]
	s, ok = getStructInfo(cls.javaName)
	if !ok {
		return nil, cls, jerrors.Errorf("can not find go type name %s in registry", clsName)
	}

	return s.typ, cls, nil
}

func (d *Decoder) decEnum(javaName string, flag int32) (JavaEnum, error) {
	var (
		err       error
		enumName  string
		ok        bool
		info      structInfo
		enumValue JavaEnum
	)
	enumName, err = d.decString(TAG_READ) // java enum class member is "name"
	if err != nil {
		return InvalidJavaEnum, jerrors.Annotate(err, "decString for decJavaEnum")
	}
	info, ok = getStructInfo(javaName)
	if !ok {
		return InvalidJavaEnum, jerrors.Errorf("getStructInfo(javaName:%s) = false", javaName)
	}

	enumValue = info.inst.(POJOEnum).EnumValue(enumName)
	d.appendRefs(enumValue)
	return enumValue, nil
}

func (d *Decoder) decObject(flag int32) (interface{}, error) {
	var (
		tag byte
		idx int32
		err error
		typ reflect.Type
		cls classInfo
	)

	if flag != TAG_READ {
		tag = byte(flag)
	} else {
		tag, _ = d.readByte()
	}

	switch {
	case tag == BC_NULL:
		return nil, nil
	case tag == BC_REF:
		return d.decRef(int32(tag))
	case tag == BC_OBJECT_DEF:
		clsDef, err := d.decClassDef()
		if err != nil {
			return nil, jerrors.Annotate(err, "decObject->decClassDef byte double")
		}
		cls, _ = clsDef.(classInfo)
		//add to slice
		d.appendClsDef(cls)

		return d.Decode()

	case tag == BC_OBJECT:
		idx, err = d.decInt32(TAG_READ)
		if err != nil {
			return nil, err
		}

		typ, cls, err = d.getStructDefByIndex(int(idx))
		if err != nil {
			return nil, err
		}
		if typ.Implements(javaEnumType) {
			return d.decEnum(cls.javaName, TAG_READ)
		}

		return d.decInstance(typ, cls)

	case BC_OBJECT_DIRECT <= tag && tag <= (BC_OBJECT_DIRECT+OBJECT_DIRECT_MAX):
		typ, cls, err = d.getStructDefByIndex(int(tag - BC_OBJECT_DIRECT))
		if err != nil {
			return nil, err
		}
		if typ.Implements(javaEnumType) {
			return d.decEnum(cls.javaName, TAG_READ)
		}

		return d.decInstance(typ, cls)

	default:
		return nil, jerrors.Errorf("decObject illegal object type tag:%+v", tag)
	}
}
