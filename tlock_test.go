package tlock_test

import (
	"bytes"
	"context"
	_ "embed" // Calls init function.
	"errors"
	"os"
	"testing"
	"time"

	"github.com/drand/tlock"
	"github.com/drand/tlock/data_encrypters/aead"
	"github.com/drand/tlock/encoders/base"
	"github.com/drand/tlock/networks/http"
)

var (
	//go:embed test_artifacts/data.txt
	dataFile []byte
)

const (
	testnetHost      = "http://pl-us.testnet.drand.sh/"
	testnetChainHash = "7672797f548f3f4748ac4bf3352fc6c6b6468c9ad40ad456a397545c6e2df5bf"
)

func Test_EarlyDecryptionWithDuration(t *testing.T) {
	network := http.NewNetwork(testnetHost, testnetChainHash)
	ctx := context.Background()

	// =========================================================================
	// Encrypt

	// Read the plaintext data to be encrypted.
	in, err := os.Open("test_artifacts/data.txt")
	if err != nil {
		t.Fatalf("reader error %s", err)
	}
	defer in.Close()

	// Write the encoded information to this buffer.
	var cipherData bytes.Buffer

	// Enough duration to check for an non-existing beacon.
	duration := 10 * time.Second

	tl := tlock.NewEncrypter(network, aead.DataEncrypter{}, base.Encoder{})

	roundNumber, err := network.RoundNumber(ctx, time.Now().Add(duration))
	if err != nil {
		t.Fatalf("round by duration: %s", err)
	}

	err = tl.Encrypt(ctx, &cipherData, in, roundNumber, false)
	if err != nil {
		t.Fatalf("encrypt with duration error %s", err)
	}

	// =========================================================================
	// Decrypt

	// Write the decoded information to this buffer.
	var plainData bytes.Buffer

	// We DO NOT wait for the future beacon to exist.
	err = tlock.NewDecrypter(network, aead.DataDecrypter{}, base.Decoder{}).Decrypt(ctx, &plainData, &cipherData, false)
	if err == nil {
		t.Fatal("expecting decrypt error")
	}

	if !errors.Is(err, tlock.ErrTooEarly) {
		t.Fatalf("expecting decrypt error to contain '%s'; got %s", tlock.ErrTooEarly, err)
	}
}

func Test_EarlyDecryptionWithRound(t *testing.T) {
	network := http.NewNetwork(testnetHost, testnetChainHash)
	ctx := context.Background()

	// =========================================================================
	// Encrypt

	// Read the plaintext data to be encrypted.
	in, err := os.Open("test_artifacts/data.txt")
	if err != nil {
		t.Fatalf("reader error %s", err)
	}
	defer in.Close()

	futureRound, err := network.RoundNumber(ctx, time.Now().Add(1*time.Minute))
	if err != nil {
		t.Fatalf("client: %s", err)
	}

	// Write the encoded information to this buffer.
	var cipherData bytes.Buffer

	tl := tlock.NewEncrypter(network, aead.DataEncrypter{}, base.Encoder{})

	err = tl.Encrypt(context.Background(), &cipherData, in, futureRound, false)
	if err != nil {
		t.Fatalf("encrypt with round error %s", err)
	}

	// =========================================================================
	// Decrypt

	// Write the decoded information to this buffer.
	var plainData bytes.Buffer

	// We DO NOT wait for the future beacon to exist.
	err = tlock.NewDecrypter(network, aead.DataDecrypter{}, base.Decoder{}).Decrypt(ctx, &plainData, &cipherData, false)
	if err == nil {
		t.Fatal("expecting decrypt error")
	}

	if !errors.Is(err, tlock.ErrTooEarly) {
		t.Fatalf("expecting decrypt error to contain '%s'; got %s", tlock.ErrTooEarly, err)
	}
}

func Test_EncryptionWithDuration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}

	network := http.NewNetwork(testnetHost, testnetChainHash)
	ctx := context.Background()

	// =========================================================================
	// Encrypt

	// Read the plaintext data to be encrypted.
	in, err := os.Open("test_artifacts/data.txt")
	if err != nil {
		t.Fatalf("reader error %s", err)
	}
	defer in.Close()

	// Write the encoded information to this buffer.
	var cipherData bytes.Buffer

	// Enough duration to check for an non-existing beacon.
	duration := 4 * time.Second

	tl := tlock.NewEncrypter(network, aead.DataEncrypter{}, base.Encoder{})

	roundNumber, err := network.RoundNumber(ctx, time.Now().Add(duration))
	if err != nil {
		t.Fatalf("round by duration: %s", err)
	}

	err = tl.Encrypt(ctx, &cipherData, in, roundNumber, false)
	if err != nil {
		t.Fatalf("encrypt with duration error %s", err)
	}

	// =========================================================================
	// Decrypt

	time.Sleep(5 * time.Second)

	// Write the decoded information to this buffer.
	var plainData bytes.Buffer

	err = tlock.NewDecrypter(network, aead.DataDecrypter{}, base.Decoder{}).Decrypt(ctx, &plainData, &cipherData, false)
	if err != nil {
		t.Fatalf("unexpected error %s", err)
	}

	if !bytes.Equal(plainData.Bytes(), dataFile) {
		t.Fatalf("decrypted file is invalid; expected %d; got %d", len(dataFile), len(plainData.Bytes()))
	}
}

func Test_EncryptionWithRound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}

	network := http.NewNetwork(testnetHost, testnetChainHash)
	ctx := context.Background()

	// =========================================================================
	// Encrypt

	// Read the plaintext data to be encrypted.
	in, err := os.Open("test_artifacts/data.txt")
	if err != nil {
		t.Fatalf("reader error %s", err)
	}
	defer in.Close()

	// Write the encoded information to this buffer.
	var cipherData bytes.Buffer

	futureRound, err := network.RoundNumber(ctx, time.Now().Add(6*time.Second))
	if err != nil {
		t.Fatalf("client: %s", err)
	}

	err = tlock.NewEncrypter(network, aead.DataEncrypter{}, base.Encoder{}).Encrypt(ctx, &cipherData, in, futureRound, false)
	if err != nil {
		t.Fatalf("encrypt with duration error %s", err)
	}

	// =========================================================================
	// Decrypt

	var plainData bytes.Buffer

	// Wait for the future beacon to exist.
	time.Sleep(10 * time.Second)

	err = tlock.NewDecrypter(network, aead.DataDecrypter{}, base.Decoder{}).Decrypt(ctx, &plainData, &cipherData, false)
	if err != nil {
		t.Fatalf("unexpected error %s", err)
	}

	if !bytes.Equal(plainData.Bytes(), dataFile) {
		t.Fatalf("decrypted file is invalid; expected %d; got %d", len(dataFile), len(plainData.Bytes()))
	}
}
