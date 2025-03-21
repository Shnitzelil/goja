package goja_test // this is on purpose in a separate package

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Shnitzelil/goja"
)

type simpleComboResolver struct {
	mu           sync.Mutex
	cache        map[string]cacheElement
	reverseCache map[goja.ModuleRecord]string
	fs           fs.FS
	custom       func(interface{}, string) (goja.ModuleRecord, error)
}
type cacheElement struct {
	m   goja.ModuleRecord
	err error
}

func newSimpleComboResolver() *simpleComboResolver {
	return &simpleComboResolver{cache: make(map[string]cacheElement), reverseCache: make(map[goja.ModuleRecord]string)}
}

func (s *simpleComboResolver) resolve(referencingScriptOrModule interface{}, specifier string) (goja.ModuleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.cache[specifier]
	if ok {
		return k.m, k.err
	}
	if strings.HasPrefix(specifier, "custom:") {
		p, err := s.custom(referencingScriptOrModule, specifier)
		s.cache[specifier] = cacheElement{m: p, err: err}
		return p, err
	}
	b, err := fs.ReadFile(s.fs, specifier)
	if err != nil {
		s.cache[specifier] = cacheElement{err: err}
		return nil, err
	}
	p, err := goja.ParseModule(specifier, string(b), s.resolve)
	if err != nil {
		s.cache[specifier] = cacheElement{err: err}
		return nil, err
	}
	s.cache[specifier] = cacheElement{m: p}
	s.reverseCache[p] = specifier
	return p, nil
}

type unresolvedBinding struct {
	module  string
	bidning string
}

func TestNotSourceModulesBigTest(t *testing.T) {
	t.Parallel()
	resolver := newSimpleComboResolver()
	resolver.custom = func(_ interface{}, specifier string) (goja.ModuleRecord, error) {
		switch specifier {
		case "custom:coolstuff":
			return &simpleModuleImpl{}, nil
		case "custom:coolstuff2":
			return &cyclicModuleImpl{
				resolve:          resolver.resolve,
				requestedModules: []string{"custom:coolstuff3", "custom:coolstuff"},
				exports: map[string]unresolvedBinding{
					"coolStuff": {
						bidning: "coolStuff",
						module:  "custom:coolstuff",
					},
					"otherCoolStuff": { // request it from third module which will request it back from us
						bidning: "coolStuff",
						module:  "custom:coolstuff3",
					},
				},
			}, nil
		case "custom:coolstuff3":
			return &cyclicModuleImpl{
				resolve:          resolver.resolve,
				requestedModules: []string{"custom:coolstuff2"},
				exports: map[string]unresolvedBinding{
					"coolStuff": { // request it back from the module
						bidning: "coolStuff",
						module:  "custom:coolstuff2",
					},
				},
			}, nil
		default:
			return nil, fmt.Errorf("unknown module %q", specifier)
		}
	}
	mapfs := make(fstest.MapFS)
	mapfs["main.js"] = &fstest.MapFile{
		Data: []byte(`
        import {coolStuff} from "custom:coolstuff";
        import {coolStuff as coolStuff3, otherCoolStuff} from "custom:coolstuff2";
        if (coolStuff != 5) {
            throw "coolStuff isn't a 5 it is a "+ coolStuff
        }
        if (coolStuff3 != 5) {
            throw "coolStuff3 isn't a 5 it is a "+ coolStuff3
        }
        if (otherCoolStuff != 5) {
            throw "otherCoolStuff isn't a 5 it is a "+ otherCoolStuff
        }
        globalThis.s = true
        `),
	}
	resolver.fs = mapfs
	m, err := resolver.resolve(nil, "main.js")
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	p := m.(*goja.SourceTextModuleRecord)

	err = p.Link()
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	vm := goja.New()
	promise := vm.CyclicModuleRecordEvaluate(p, resolver.resolve)
	if promise.State() != goja.PromiseStateFulfilled {
		err := promise.Result().Export().(error)
		t.Fatalf("got error %s", err)
	}
	if s := vm.GlobalObject().Get("s"); s == nil || !s.ToBoolean() {
		t.Fatalf("test didn't run till the end")
	}
}

func TestNotSourceModulesBigTestDynamicImport(t *testing.T) {
	t.Parallel()
	resolver := newSimpleComboResolver()
	resolver.custom = func(_ interface{}, specifier string) (goja.ModuleRecord, error) {
		switch specifier {
		case "custom:coolstuff":
			return &simpleModuleImpl{}, nil
		case "custom:coolstuff2":
			return &cyclicModuleImpl{
				resolve:          resolver.resolve,
				requestedModules: []string{"custom:coolstuff3", "custom:coolstuff"},
				exports: map[string]unresolvedBinding{
					"coolStuff": {
						bidning: "coolStuff",
						module:  "custom:coolstuff",
					},
					"otherCoolStuff": { // request it from third module which will request it back from us
						bidning: "coolStuff",
						module:  "custom:coolstuff3",
					},
				},
			}, nil
		case "custom:coolstuff3":
			return &cyclicModuleImpl{
				resolve:          resolver.resolve,
				requestedModules: []string{"custom:coolstuff2"},
				exports: map[string]unresolvedBinding{
					"coolStuff": { // request it back from the module
						bidning: "coolStuff",
						module:  "custom:coolstuff2",
					},
				},
			}, nil
		default:
			return nil, fmt.Errorf("unknown module %q", specifier)
		}
	}
	mapfs := make(fstest.MapFS)
	mapfs["main.js"] = &fstest.MapFile{
		Data: []byte(`
        Promise.all([import("custom:coolstuff"), import("custom:coolstuff2")]).then((res)=> {
            let coolStuff = res[0].coolStuff
            let coolStuff3 = res[1].coolStuff
            let otherCoolStuff = res[1].otherCoolStuff

            if (coolStuff != 5) {
                throw "coolStuff isn't a 5 it is a "+ coolStuff
            }
            if (coolStuff3 != 5) {
                throw "coolStuff3 isn't a 5 it is a "+ coolStuff3
            }
            if (otherCoolStuff != 5) {
                throw "otherCoolStuff isn't a 5 it is a "+ otherCoolStuff
            }
            globalThis.s = true;
        })`),
	}
	resolver.fs = mapfs
	m, err := resolver.resolve(nil, "main.js")
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	p := m.(*goja.SourceTextModuleRecord)
	err = p.Link()
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	vm := goja.New()
	eventLoopQueue := make(chan func(), 2) // the most basic and likely buggy event loop
	vm.SetPromiseRejectionTracker(func(p *goja.Promise, operation goja.PromiseRejectionOperation) {
		t.Fatal(p.Result())
	})
	vm.SetImportModuleDynamically(func(referencingScriptOrModule interface{}, specifierValue goja.Value, promiseCapability interface{}) {
		specifier := specifierValue.String()
		go func() {
			m, err := resolver.resolve(referencingScriptOrModule, specifier)

			eventLoopQueue <- func() {
				defer vm.RunString("") // FIXME haxx // the specification kind of doesn't have a solutioo for htis it seems
				vm.FinishLoadingImportModule(referencingScriptOrModule, specifierValue, promiseCapability, m, err)
			}
		}()
	})
	promise := vm.CyclicModuleRecordEvaluate(p, resolver.resolve)
	// TODO fix
	if promise.State() != goja.PromiseStateFulfilled {
		err := promise.Result().Export().(error)
		t.Fatalf("got error %s", err)
	}
	const timeout = time.Millisecond * 1000
	for {
		if s := vm.GlobalObject().Get("s"); s != nil {
			if !s.ToBoolean() {
				t.Fatal("s has wrong value false")
			}
			return
		}
		select {
		case fn := <-eventLoopQueue:
			fn()
		case <-time.After(timeout):
			t.Fatalf("nothing happened in %s :(", timeout)
		}
	}
}

// START of simple module implementation
type simpleModuleImpl struct{}

var _ goja.ModuleRecord = &simpleModuleImpl{}

func (s *simpleModuleImpl) Link() error {
	// this does nothing on this
	return nil
}

func (s *simpleModuleImpl) ResolveExport(exportName string, resolveset ...goja.ResolveSetElement) (*goja.ResolvedBinding, bool) {
	if exportName == "coolStuff" {
		return &goja.ResolvedBinding{
			BindingName: exportName,
			Module:      s,
		}, false
	}
	return nil, false
}

func (s *simpleModuleImpl) Evaluate(rt *goja.Runtime) *goja.Promise {
	p, res, _ := rt.NewPromise()
	res(&simpleModuleInstanceImpl{rt: rt})
	return p
}

func (s *simpleModuleImpl) GetExportedNames(callback func([]string), records ...goja.ModuleRecord) bool {
	callback([]string{"coolStuff"})
	return true
}

type simpleModuleInstanceImpl struct {
	rt *goja.Runtime
}

func (si *simpleModuleInstanceImpl) GetBindingValue(exportName string) goja.Value {
	if exportName == "coolStuff" {
		return si.rt.ToValue(5)
	}
	return nil
}

// START of cyclic module implementation
type cyclicModuleImpl struct {
	requestedModules []string
	exports          map[string]unresolvedBinding
	resolve          goja.HostResolveImportedModuleFunc
}

var _ goja.CyclicModuleRecord = &cyclicModuleImpl{}

func (s *cyclicModuleImpl) InitializeEnvironment() error {
	return nil
}

func (s *cyclicModuleImpl) Instantiate(_ *goja.Runtime) (goja.CyclicModuleInstance, error) {
	return &cyclicModuleInstanceImpl{module: s}, nil
}

func (s *cyclicModuleImpl) RequestedModules() []string {
	return s.requestedModules
}

func (s *cyclicModuleImpl) Link() error {
	// this does nothing on this
	return nil
}

func (s *cyclicModuleImpl) Evaluate(rt *goja.Runtime) *goja.Promise {
	return rt.CyclicModuleRecordEvaluate(s, s.resolve)
}

func (s *cyclicModuleImpl) ResolveExport(exportName string, resolveset ...goja.ResolveSetElement) (*goja.ResolvedBinding, bool) {
	b, ok := s.exports[exportName]
	if !ok {
		return nil, false
	}

	m, err := s.resolve(s, b.module)
	if err != nil {
		panic(err)
	}

	return &goja.ResolvedBinding{
		Module:      m,
		BindingName: b.bidning,
	}, false
}

func (s *cyclicModuleImpl) GetExportedNames(callback func([]string), records ...goja.ModuleRecord) bool {
	result := make([]string, 0, len(s.exports))
	for k := range s.exports {
		result = append(result, k)
	}
	sort.Strings(result)
	callback(result)
	return true
}

type cyclicModuleInstanceImpl struct {
	rt     *goja.Runtime
	module *cyclicModuleImpl
}

func (si *cyclicModuleInstanceImpl) HasTLA() bool {
	return false
}

func (si *cyclicModuleInstanceImpl) ExecuteModule(rt *goja.Runtime, _, _ func(interface{}) error) (goja.CyclicModuleInstance, error) {
	si.rt = rt
	return si, nil
}

func (si *cyclicModuleInstanceImpl) GetBindingValue(exportName string) goja.Value {
	b, ambigious := si.module.ResolveExport(exportName)
	if ambigious || b == nil {
		panic("fix this")
	}
	return si.rt.GetModuleInstance(b.Module).GetBindingValue(exportName)
}

func TestSourceMetaImport(t *testing.T) {
	t.Parallel()
	resolver := newSimpleComboResolver()
	mapfs := make(fstest.MapFS)
	mapfs["main.js"] = &fstest.MapFile{
		Data: []byte(`
        import { meta } from "b.js"

        if (meta.url != "file:///b.js") {
            throw "wrong url " + meta.url + " for b.js"
        }

        if (import.meta.url != "file:///main.js") {
            throw "wrong url " + import.meta.url + " for main.js"
        }
        `),
	}
	mapfs["b.js"] = &fstest.MapFile{
		Data: []byte(`
        export var meta = import.meta
        `),
	}
	resolver.fs = mapfs
	m, err := resolver.resolve(nil, "main.js")
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	p := m.(*goja.SourceTextModuleRecord)

	err = p.Link()
	if err != nil {
		t.Fatalf("got error %s", err)
	}
	vm := goja.New()
	vm.SetGetImportMetaProperties(func(m goja.ModuleRecord) []goja.MetaProperty {
		specifier, ok := resolver.reverseCache[m]
		if !ok {
			panic("we got import.meta for module that wasn't imported")
		}
		return []goja.MetaProperty{
			{
				Key:   "url",
				Value: vm.ToValue("file:///" + specifier),
			},
		}
	})
	promise := m.Evaluate(vm)
	if promise.State() != goja.PromiseStateFulfilled {
		err := promise.Result().Export().(error)
		t.Fatalf("got error %s", err)
	}
}
