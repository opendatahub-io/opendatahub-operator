/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"testing"

	. "github.com/onsi/gomega"
)

func TestIntermediateCiphersAreValid(t *testing.T) {
	g := NewWithT(t)
	g.Expect(intermediateCiphers).NotTo(BeEmpty())
	for _, id := range intermediateCiphers {
		g.Expect(tls.CipherSuiteName(id)).NotTo(BeEmpty(), "unknown cipher suite ID %d", id)
	}
}

func TestHardenedDefaultsTLSConfig(t *testing.T) {
	g := NewWithT(t)
	cfg := &tls.Config{}
	cfg.MinVersion = tls.VersionTLS12
	cfg.CipherSuites = intermediateCiphers
	cfg.NextProtos = []string{"h2", "http/1.1"}

	g.Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	g.Expect(cfg.CipherSuites).To(Equal(intermediateCiphers))
	g.Expect(cfg.NextProtos).To(Equal([]string{"h2", "http/1.1"}))
}
