package tlock

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"

	"filippo.io/age"
	"github.com/drand/drand/chain"
	"github.com/drand/drand/common/scheme"
	bls "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/encrypt/ibe"
)

// These constants define the size of the different CipherDEK fields.
const (
	kyberPointLen = 48
	cipherVLen    = 16
	cipherWLen    = 16
)

// cipherDEK represents the encrypted data encryption key (DEK) needed to decrypt
// the cipher data.
type cipherDEK struct {
	kyberPoint []byte
	cipherV    []byte
	cipherW    []byte
}

// =============================================================================

// tleRecipient implements the age Recipient interface. This is used to encrypt
// data with the age Encrypt API.
type tleRecipient struct {
	round   uint64
	network Network
}

// Wrap is called by the age Encrypt API and is provided the DEK generated by
// age that is used for encrypting/decrypting data. Inside of Wrap we encrypt
// the DEK using time lock encryption.
func (t *tleRecipient) Wrap(fileKey []byte) ([]*age.Stanza, error) {
	id, err := calculateEncryptionID(t.round)
	if err != nil {
		return nil, fmt.Errorf("round by number: %w", err)
	}

	cipherText, err := ibe.Encrypt(bls.NewBLS12381Suite(), t.network.PublicKey(), id, fileKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt dek: %w", err)
	}

	kyberPoint, err := cipherText.U.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal kyber point: %w", err)
	}

	cipherDEK := make([]byte, kyberPointLen+cipherVLen+cipherWLen)
	copy(cipherDEK, kyberPoint)
	copy(cipherDEK[kyberPointLen:], cipherText.V)
	copy(cipherDEK[kyberPointLen+cipherVLen:], cipherText.W)

	stanza := age.Stanza{
		Type: "tlock",
		Args: []string{strconv.FormatUint(t.round, 10), t.network.ChainHash()},
		Body: cipherDEK,
	}

	return []*age.Stanza{&stanza}, nil
}

// calculateEncryptionID will generate the id required for encryption.
func calculateEncryptionID(roundNumber uint64) ([]byte, error) {
	h := sha256.New()
	if _, err := h.Write(chain.RoundToBytes(roundNumber)); err != nil {
		return nil, fmt.Errorf("sha256 write: %w", err)
	}

	return h.Sum(nil), nil
}

// =============================================================================

// tleIdentity implements the age Identity interface. This is used to decrypt
// data with the age Decrypt API.
type tleIdentity struct {
	network Network
}

// Unwrap is called by the age Decrypt API and is provided the DEK that was time
// lock encrypted by the Wrap function via the Stanza. Inside of Unwrap we decrypt
// the DEK and provide back to age.
func (t *tleIdentity) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	if len(stanzas) != 1 {
		return nil, errors.New("check stanzas length: should be one")
	}

	stanza := stanzas[0]

	if stanza.Type != "tlock" {
		return nil, fmt.Errorf("check stanza type: wrong type: %w", age.ErrIncorrectIdentity)
	}

	if len(stanza.Args) != 2 {
		return nil, fmt.Errorf("check stanza args: should be two: %w", age.ErrIncorrectIdentity)
	}

	blockRound, err := strconv.ParseUint(stanza.Args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse block round: %w", err)
	}

	if t.network.ChainHash() != stanza.Args[1] {
		return nil, errors.New("wrong chainhash")
	}

	cipherDEK, err := parseCipherDEK(stanza.Body)
	if err != nil {
		return nil, fmt.Errorf("parse cipher dek: %w", err)
	}

	fileKey, err := decryptDEK(cipherDEK, t.network, blockRound)
	if err != nil {
		return nil, fmt.Errorf("decrypt dek: %w", err)
	}

	return fileKey, nil
}

// parseCipherDEK parses the stanzaBody constructed in the Wrap method back to
// the original cipherDEK.
func parseCipherDEK(stanzaBody []byte) (cipherDEK, error) {
	expLen := kyberPointLen + cipherVLen + cipherWLen
	if len(stanzaBody) != expLen {
		return cipherDEK{}, fmt.Errorf("incorrect length: exp: %d got: %d", expLen, len(stanzaBody))
	}

	kyberPoint := make([]byte, kyberPointLen)
	copy(kyberPoint, stanzaBody[:kyberPointLen])

	cipherV := make([]byte, cipherVLen)
	copy(cipherV, stanzaBody[kyberPointLen:kyberPointLen+cipherVLen])

	cipherW := make([]byte, cipherVLen)
	copy(cipherW, stanzaBody[kyberPointLen+cipherVLen:])

	cd := cipherDEK{
		kyberPoint: kyberPoint,
		cipherV:    cipherV,
		cipherW:    cipherW,
	}

	return cd, nil
}

// decryptDEK attempts to decrypt an encrypted DEK against the provided network
// for the specified round.
func decryptDEK(cipherDEK cipherDEK, network Network, roundNumber uint64) (fileKey []byte, err error) {
	id, ready := network.IsReadyToDecrypt(roundNumber)
	if !ready {
		return nil, ErrTooEarly
	}

	b := chain.Beacon{
		Round:     roundNumber,
		Signature: id,
	}
	sch := scheme.Scheme{
		ID:              scheme.UnchainedSchemeID,
		DecouplePrevSig: true,
	}
	if err := chain.NewVerifier(sch).VerifyBeacon(b, network.PublicKey()); err != nil {
		return nil, fmt.Errorf("verify beacon: %w", err)
	}

	var signature bls.KyberG2
	if err := signature.UnmarshalBinary(id); err != nil {
		return nil, fmt.Errorf("unmarshal kyber G2: %w", err)
	}

	var kyberPoint bls.KyberG1
	if err := kyberPoint.UnmarshalBinary(cipherDEK.kyberPoint); err != nil {
		return nil, fmt.Errorf("unmarshal kyber G1: %w", err)
	}

	cipherText := ibe.Ciphertext{
		U: &kyberPoint,
		V: cipherDEK.cipherV,
		W: cipherDEK.cipherW,
	}

	fileKey, err = ibe.Decrypt(bls.NewBLS12381Suite(), &signature, &cipherText)
	if err != nil {
		return nil, fmt.Errorf("decrypt dek: %w", err)
	}

	return fileKey, nil
}
