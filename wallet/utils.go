package wallet

import (
	"encoding/binary"
	"io"
	"math/big"
	"bytes"
	"github.com/ninjadotorg/cash-prototype/common"
	"github.com/ninjadotorg/cash-prototype/common/base58"
	"golang.org/x/crypto/ripemd160"
)

//
// Hashes
//

func hashRipeMD160(data []byte) ([]byte, error) {
	hasher := ripemd160.New()
	_, err := io.WriteString(hasher, string(data))
	if err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}

func hash160(data []byte) ([]byte, error) {
	hash1 := common.HashB(data)
	hash2, err := hashRipeMD160(hash1)
	if err != nil {
		return nil, err
	}

	return hash2, nil
}

//
// Encoding
//

func addChecksumToBytes(data []byte) ([]byte, error) {
	checksum := base58.ChecksumFirst4Bytes(data)
	return append(data, checksum...), nil
}

// Keys
func compressPublicKey(x *big.Int, y *big.Int) []byte {
	var key bytes.Buffer

	// Write header; 0x2 for even y value; 0x3 for odd
	key.WriteByte(byte(0x2) + byte(y.Bit(0)))

	// Write X coord; Pad the key so x is aligned with the LSB. Pad size is key length - header size (1) - xBytes size
	xBytes := x.Bytes()
	for i := 0; i < (PublicKeyCompressedLength - 1 - len(xBytes)); i++ {
		key.WriteByte(0x0)
	}
	key.Write(xBytes)

	return key.Bytes()
}

//
//// As described at https://crypto.stackexchange.com/a/8916
//func expandPublicKey(key []byte) (*big.Int, *big.Int) {
//	Y := big.NewInt(0)
//	X := big.NewInt(0)
//	X.SetBytes(key[1:])
//
//	// y^2 = x^3 + ax^2 + b
//	// a = 0
//	// => y^2 = x^3 + b
//	ySquared := big.NewInt(0)
//	ySquared.Exp(X, big.NewInt(3), nil)
//	ySquared.Add(ySquared, curveParams.B)
//
//	Y.ModSqrt(ySquared, curveParams.P)
//
//	Ymod2 := big.NewInt(0)
//	Ymod2.Mod(Y, big.NewInt(2))
//
//	signY := uint64(key[0]) - 2
//	if signY != Ymod2.Uint64() {
//		Y.Sub(curveParams.P, Y)
//	}
//
//	return X, Y
//}
//
//func validatePrivateKey(key []byte) error {
//	if fmt.Sprintf("%x", key) == "0000000000000000000000000000000000000000000000000000000000000000" || //if the key is zero
//		bytes.Compare(key, curveParams.N.Bytes()) >= 0 || //or is outside of the curve
//		len(key) != 32 { //or is too short
//		return ErrInvalidPrivateKey
//	}
//
//	return nil
//}
//
//func validateChildPublicKey(key []byte) error {
//	x, y := expandPublicKey(key)
//
//	if x.Sign() == 0 || y.Sign() == 0 {
//		return ErrInvalidPublicKey
//	}
//
//	return nil
//}
//

/**
Numerical
 */
func uint32Bytes(i uint32) []byte {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, i)
	return bytes
}
