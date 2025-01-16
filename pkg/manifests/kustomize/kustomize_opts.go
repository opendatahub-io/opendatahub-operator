package kustomize

import (
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

type EngineOptsFn func(engine *Engine)

func WithEngineFS(value filesys.FileSystem) EngineOptsFn {
	return func(engine *Engine) {
		engine.fs = value
	}
}

func WithEngineRenderOpts(values ...RenderOptsFn) EngineOptsFn {
	return func(engine *Engine) {
		for _, fn := range values {
			fn(&engine.renderOpts)
		}
	}
}
