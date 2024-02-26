// Code generated by counterfeiter. DO NOT EDIT.
package randomstringfakes

import (
	"sync"

	"github.com/linode/provider-ceph/internal/utils/randomstring"
)

type FakeGenerator struct {
	GenerateStub        func(string, int, *randomstring.Charset) (string, error)
	generateMutex       sync.RWMutex
	generateArgsForCall []struct {
		arg1 string
		arg2 int
		arg3 *randomstring.Charset
	}
	generateReturns struct {
		result1 string
		result2 error
	}
	generateReturnsOnCall map[int]struct {
		result1 string
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeGenerator) Generate(arg1 string, arg2 int, arg3 *randomstring.Charset) (string, error) {
	fake.generateMutex.Lock()
	ret, specificReturn := fake.generateReturnsOnCall[len(fake.generateArgsForCall)]
	fake.generateArgsForCall = append(fake.generateArgsForCall, struct {
		arg1 string
		arg2 int
		arg3 *randomstring.Charset
	}{arg1, arg2, arg3})
	stub := fake.GenerateStub
	fakeReturns := fake.generateReturns
	fake.recordInvocation("Generate", []interface{}{arg1, arg2, arg3})
	fake.generateMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2, arg3)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *FakeGenerator) GenerateCallCount() int {
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	return len(fake.generateArgsForCall)
}

func (fake *FakeGenerator) GenerateCalls(stub func(string, int, *randomstring.Charset) (string, error)) {
	fake.generateMutex.Lock()
	defer fake.generateMutex.Unlock()
	fake.GenerateStub = stub
}

func (fake *FakeGenerator) GenerateArgsForCall(i int) (string, int, *randomstring.Charset) {
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	argsForCall := fake.generateArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2, argsForCall.arg3
}

func (fake *FakeGenerator) GenerateReturns(result1 string, result2 error) {
	fake.generateMutex.Lock()
	defer fake.generateMutex.Unlock()
	fake.GenerateStub = nil
	fake.generateReturns = struct {
		result1 string
		result2 error
	}{result1, result2}
}

func (fake *FakeGenerator) GenerateReturnsOnCall(i int, result1 string, result2 error) {
	fake.generateMutex.Lock()
	defer fake.generateMutex.Unlock()
	fake.GenerateStub = nil
	if fake.generateReturnsOnCall == nil {
		fake.generateReturnsOnCall = make(map[int]struct {
			result1 string
			result2 error
		})
	}
	fake.generateReturnsOnCall[i] = struct {
		result1 string
		result2 error
	}{result1, result2}
}

func (fake *FakeGenerator) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeGenerator) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ randomstring.Generator = new(FakeGenerator)