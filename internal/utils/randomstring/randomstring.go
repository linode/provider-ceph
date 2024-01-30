//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

// Package randomstring provides utilities for generating random strings.
package randomstring

import (
	"crypto/rand"
	"math/big"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
)

var (
	errRandomStringLengthTooShort   = errors.New("the random string length is too short")
	errRandomStringGenerationFailed = errors.New("failed generating random string")
)

// Charset is a wrapper for a charset that needs to be provided for random
// string generation. It prevents the need to create a big.Int multiple times.
type Charset struct {
	charset      string
	bigIntLength *big.Int
}

func NewCharset(charset string) *Charset {
	return &Charset{
		charset:      charset,
		bigIntLength: big.NewInt(int64(len(charset))),
	}
}

//counterfeiter:generate . Generator
type Generator interface {
	Generate(prefix string, length int, charset *Charset) (string, error)
}

type StandardGenerator struct{}

func (StandardGenerator) Generate(prefix string, length int, charset *Charset) (string, error) {
	generatedLength := length - len(prefix)
	if generatedLength <= 0 {
		return "", errRandomStringLengthTooShort
	}

	str := make([]byte, generatedLength)
	for i := range str {
		j, err := rand.Int(rand.Reader, charset.bigIntLength)
		if err != nil {
			return "", errors.Wrap(err, errRandomStringGenerationFailed.Error())
		}
		str[i] = charset.charset[j.Int64()]
	}

	return prefix + string(str), nil
}
