# ankh

`ankh` is a CLI tool that helps manage larger, multi-tenant Kubernetes
clusters. It is currently under development.

## Installing

Eventually we will publish a set of precompiled binaries, but for the time
being it's best to install go 1.9+ and install the source and binary with:

		go get github.com/jondlm/ankh

## Contributing

Here are the recommended steps for contributing to ankh:

		# Ensure you have go 1.9+ installed with `brew install go` on a Mac
		go get github.com/jondlm/ankh
		cd $GOPATH/src/github.com/jondlm/ankh
		go run ankh.go
