//go:build tools
// +build tools

package tools

import (
	_ "github.com/fzipp/gocyclo/cmd/gocyclo"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/google/ko"
	_ "github.com/mikefarah/yq/v4"
	_ "github.com/mitchellh/golicense"
	_ "github.com/norwoodj/helm-docs/cmd/helm-docs"
	_ "github.com/onsi/ginkgo/ginkgo"
	_ "github.com/sigstore/cosign/cmd/cosign"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
