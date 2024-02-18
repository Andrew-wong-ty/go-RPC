package rpc

import (
	"bytes"
	"encoding/gob"
	"errors"
	"reflect"
)

// EncodeToBytes encodes a object to bytes
func EncodeToBytes(data any) ([]byte, error) {
	var buf bytes.Buffer
	bufEnc := gob.NewEncoder(&buf)
	err := bufEnc.Encode(data)
	if err != nil {
		return nil, errors.New("encode failed")
	}
	return buf.Bytes(), err
}

// DecodeToReqMsg decode bytes to RequestMsg
func DecodeToReqMsg(data []byte) (*RequestMsg, error) {
	bufD := bytes.NewBuffer(data)
	bufDec := gob.NewDecoder(bufD)
	var reqOri RequestMsg
	err := bufDec.Decode(&reqOri)
	if err != nil {
		return nil, errors.New("decode to RequestMsg failed")
	}
	return &reqOri, nil
}

// DecodeToReplyMsg decode bytes to ReplyMsg
func DecodeToReplyMsg(data []byte) (*ReplyMsg, error) {
	bufD := bytes.NewBuffer(data)
	bufDec := gob.NewDecoder(bufD)
	var replyOri ReplyMsg
	err := bufDec.Decode(&replyOri)
	if err != nil {
		return nil, errors.New("decode to ReplyMsg failed")
	}
	return &replyOri, nil
}

// Interfaces2Values turn a slice of interface{} to []reflect.Value
func Interfaces2Values(ifs []interface{}) []reflect.Value {
	values := make([]reflect.Value, len(ifs))
	for i, argIf := range ifs {
		values[i] = reflect.ValueOf(argIf)
	}
	return values
}

// Values2Interfaces turn a slice of reflect.Value to []interface{}
func Values2Interfaces(values []reflect.Value) []interface{} {
	ifs := make([]interface{}, len(values))
	for i, value := range values {
		ifs[i] = value.Interface()
	}
	return ifs
}

// getFuncPrototypeByStructField generates a function prototype string using interface's field
// And example of function prototype: "FunctionName:(Arg1,Arg2,..)->ReturnType1, ReturnType2"
func getFuncPrototypeByStructField(sf reflect.StructField) string {
	fieldType := sf.Type
	args := make([]string, 0)
	rets := make([]string, 0)
	for j := 0; j < fieldType.NumIn(); j++ {
		args = append(args, fieldType.In(j).Name())
	}
	for j := 0; j < fieldType.NumOut(); j++ {
		rets = append(rets, fieldType.Out(j).Name())
	}
	return prototypeString(sf.Name, args, rets)
}

// getFuncPrototypeByFuncType generates a function prototype string using object's prototype name and Type
func getFuncPrototypeByFuncType(fnName string, fnT reflect.Type) string {
	args := make([]string, 0)
	rets := make([]string, 0)
	for j := 0; j < fnT.NumIn(); j++ {
		args = append(args, fnT.In(j).Name())
	}
	for j := 0; j < fnT.NumOut(); j++ {
		rets = append(rets, fnT.Out(j).Name())
	}
	return prototypeString(fnName, args, rets)
}

// prototypeString generates a string based on function name,  argument types and return types
// for example, func Add(int, int) (int, RemoteObjectError) generates string: "func:(int,int)->(int,RemoteObjectError,)"
func prototypeString(fnName string, args []string, rets []string) string {
	str := fnName + ":("
	for _, arg := range args {
		str += arg + ","
	}
	str += ")->("
	for _, ret := range rets {
		str += ret + ","
	}
	str += ")"
	return str
}
