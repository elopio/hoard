package main

import (
	"os"

	"context"
	"io/ioutil"

	"fmt"

	"net"
	"time"

	"encoding/json"

	"io"

	"encoding/base64"

	"github.com/jawher/mow.cli"
	"github.com/monax/hoard/cmd"
	"github.com/monax/hoard/config"
	"github.com/monax/hoard/core"
	"github.com/monax/hoard/server"
	"google.golang.org/grpc"
)

func main() {
	hoarctlApp := cli.App("hoarctl",
		"Command line interface to the hoard daemon a content-addressed "+
			"deterministically encrypted blob storage system")

	dialURL := hoarctlApp.StringOpt("a address", config.DefaultListenAddress,
		"local address on which hoard is listening encoded as a URL with the "+
			"network protocol as the scheme, for example 'tcp://localhost:54192' "+
			"or 'unix:///tmp/hoard.sock'")

	// Scope a few variables to this lexical scope
	var cleartextClient core.CleartextClient
	var encryptionClient core.EncryptionClient
	var storageClient core.StorageClient
	var conn *grpc.ClientConn

	hoarctlApp.Before = func() {
		netProtocol, localAddress, err := server.SplitListenURL(*dialURL)

		conn, err = grpc.Dial(*dialURL,
			grpc.WithInsecure(),
			// We have to bugger around with this so we can dial an arbitrary net.Conn
			grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
				return net.Dial(netProtocol, localAddress)
			}))

		if err != nil {
			fatalf("Could not dial hoard server on %s: %v", *dialURL, err)
		}
		cleartextClient = core.NewCleartextClient(conn)
		encryptionClient = core.NewEncryptionClient(conn)
		storageClient = core.NewStorageClient(conn)
	}

	cmd.AddVersionCommand(hoarctlApp)

	hoarctlApp.Command("put",
		"Put some data read from STDIN into encrypted data store and return a reference",
		func(cmd *cli.Cmd) {
			saltString := saltOpt(cmd)

			cmd.Action = func() {
				data, err := ioutil.ReadAll(os.Stdin)
				if err != nil {
					fatalf("Could read bytes from STDIN to store: %v", err)
				}
				ref, err := cleartextClient.Put(context.Background(),
					&core.Plaintext{
						Data: data,
						Salt: parseSalt(*saltString),
					})
				if err != nil {
					fatalf("Error storing data: %v", err)
				}
				fmt.Printf("%s\n", jsonString(ref))
			}
		})

	hoarctlApp.Command("get",
		"Get some data from encrypted data store and write it to STDOUT. "+
			"Must have the JSON reference to the object passed in on STDIN (as "+
			"generated by ref or put) or the ADDRESS and SECRET_KEY provided.",
		func(cmd *cli.Cmd) {
			address := cmd.StringArg("ADDRESS", "",
				"The address of the data to retrieve as base64-encoded string")
			secretKey := cmd.StringOpt("k key", "",
				"The secret key to decrypt the data with as base64-encoded string")
			saltString := saltOpt(cmd)

			cmd.Spec = fmt.Sprintf("[--key=<SECRET_KEY>%s ADDRESS]", cmd.Spec)

			cmd.Action = func() {
				var ref *core.Reference
				var err error
				// If given address then try to read reference from arguments and option
				if address != nil && *address != "" {
					if secretKey == nil || *secretKey == "" {
						fatalf("A secret key must be provided in order to decrypt.")
					}
					ref = &core.Reference{
						Address:   readBase64(*address),
						SecretKey: readBase64(*secretKey),
						Salt:      parseSalt(*saltString),
					}
				} else {
					// if no address then read reference from JSON on STDIN
					ref, err = parseReference(os.Stdin)
					if err != nil {
						fatalf("Could read reference from STDIN to retrieve: %v", err)
					}
				}
				plaintext, err := cleartextClient.Get(context.Background(), ref)
				if err != nil {
					fatalf("Error retrieving data: %v", err)
				}
				os.Stdout.Write(plaintext.Data)
			}
		})

	hoarctlApp.Command("ref",
		"Encrypt data from STDIN and return its reference",
		func(cmd *cli.Cmd) {

			saltString := saltOpt(cmd)

			cmd.Action = func() {
				data, err := ioutil.ReadAll(os.Stdin)
				if err != nil {
					fatalf("Could read bytes from STDIN to store: %v", err)
				}
				refAndCiphertext, err := encryptionClient.Encrypt(context.Background(),
					&core.Plaintext{
						Data: data,
						Salt: parseSalt(*saltString),
					})
				if err != nil {
					fatalf("Error generating reference: %v", err)
				}
				fmt.Printf("%s\n", jsonString(refAndCiphertext.Reference))
			}
		})

	hoarctlApp.Command("encrypt",
		"Encrypt data from STDIN and output encrypted data on STDOUT",
		func(cmd *cli.Cmd) {

			saltString := saltOpt(cmd)

			cmd.Action = func() {
				data, err := ioutil.ReadAll(os.Stdin)
				if err != nil {
					fatalf("Could read bytes from STDIN to store: %v", err)
				}
				refAndCiphertext, err := encryptionClient.Encrypt(context.Background(),
					&core.Plaintext{
						Data: data,
						Salt: parseSalt(*saltString),
					})
				if err != nil {
					fatalf("Error encrypting: %v", err)
				}
				os.Stdout.Write(refAndCiphertext.Ciphertext.EncryptedData)
			}
		})

	hoarctlApp.Command("decrypt",
		"Decrypt data from STDIN and output decrypted data on STDOUT",
		func(cmd *cli.Cmd) {

			secretKey := cmd.StringOpt("k key", "",
				"The secret key to decrypt the data with as base64-encoded string")

			cmd.Spec = "--key=<secret key>"

			saltString := saltOpt(cmd)

			cmd.Action = func() {
				encryptedData, err := ioutil.ReadAll(os.Stdin)
				if err != nil {
					fatalf("Could read bytes from STDIN to store: %v", err)
				}
				plaintext, err := encryptionClient.Decrypt(context.Background(),
					&core.ReferenceAndCiphertext{
						Reference: &core.Reference{
							SecretKey: readBase64(*secretKey),
							Salt:      parseSalt(*saltString),
						},
						Ciphertext: &core.Ciphertext{
							EncryptedData: encryptedData,
						},
					})
				if err != nil {
					fatalf("Error decrypting: %v", err)
				}
				os.Stdout.Write(plaintext.Data)
			}
		})

	hoarctlApp.Command("stat",
		"Get information about the encrypted blob stored as an address from "+
			"a reference passed in on STDIN or passed as in as a single argument "+
			"as a base64 encoded string",
		func(cmd *cli.Cmd) {
			var addressBytes []byte

			address := cmd.StringArg("ADDRESS", "",
				"The address of the data to retrieve as base64-encoded string")

			cmd.Spec = "[ADDRESS]"

			cmd.Action = func() {
				// If given address use it
				if address != nil && *address != "" {
					addressBytes = readBase64(*address)
				} else {
					ref, err := parseReference(os.Stdin)
					if err != nil {
						fatalf("Could read reference from STDIN to retrieve: %v", err)
					}
					addressBytes = ref.Address
				}
				statInfo, err := storageClient.Stat(context.Background(),
					&core.Address{Address: addressBytes})
				if err != nil {
					fatalf("Error querying data: %v", err)
				}
				fmt.Printf("%s\n", jsonString(statInfo))
			}
		})

	hoarctlApp.Command("insert",
		"Insert encrypted (presumably) data on STDIN directly into store at "+
			"its address which is written to STDOUT.",
		func(cmd *cli.Cmd) {
			cmd.Action = func() {
				data, err := ioutil.ReadAll(os.Stdin)
				if err != nil {
					fatalf("Could read bytes from STDIN to store: %v", err)
				}
				// If given address use it
				address, err := storageClient.Push(context.Background(),
					&core.Ciphertext{EncryptedData: data})
				if err != nil {
					fatalf("Error querying data: %v", err)
				}
				fmt.Printf("%s\n", jsonString(address))
			}
		})

	hoarctlApp.Command("cat",
		"Retrieve the encrypted blob stored as an address from "+
			"a reference passed in on STDIN or passed as in as a single argument "+
			"as a base64 encoded string",
		func(cmd *cli.Cmd) {
			var addressBytes []byte
			var err error

			address := cmd.StringArg("ADDRESS", "",
				"The address of the data to retrieve as base64-encoded string")

			cmd.Spec = "[ADDRESS]"

			cmd.Action = func() {
				// If given address use it
				if address != nil && *address != "" {
					addressBytes, err = base64.StdEncoding.DecodeString(*address)
					if err != nil {
						fatalf("Could not decode address '%s' as base64-encoded "+
							"string", *address)
					}
				} else {
					ref, err := parseReference(os.Stdin)
					if err != nil {
						fatalf("Could read reference from STDIN to retrieve: %v", err)
					}
					addressBytes = ref.Address
				}
				ciphertext, err := storageClient.Pull(context.Background(),
					&core.Address{Address: addressBytes})
				if err != nil {
					fatalf("Error querying data: %v", err)
				}
				os.Stdout.Write(ciphertext.EncryptedData)
			}
		})

	hoarctlApp.Run(os.Args)
}

// Since we reuse the salt option
func saltOpt(cmd *cli.Cmd) *string {
	saltString := cmd.StringOpt("s salt", "", "The salt "+
		"to use when for encryption and decryption. Will be parsed as base64 "+
		"encoded string if this is possible, otherwise will be interpreted as "+
		"the bytes of the string itself.")

	cmd.Spec += " [--salt=<base64-encoded or string salt>]"
	return saltString
}

func parseSalt(saltString string) []byte {
	if saltString == "" {
		return nil
	}
	saltBytes, err := base64.StdEncoding.DecodeString(saltString)
	if err == nil {
		return saltBytes
	}
	return ([]byte)(saltString)
}

func jsonString(v interface{}) string {
	bs, err := json.Marshal(v)
	if err != nil {
		fatalf("Could not serialise '%s' to json: %v", err)
	}
	return string(bs)

}

func parseReference(r io.Reader) (*core.Reference, error) {
	ref := new(core.Reference)
	bs, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bs, ref)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func readBase64(base64String string) []byte {
	secretKeyBytes, err := base64.StdEncoding.DecodeString(base64String)
	if err != nil {
		fatalf("Could not decode '%s' as base64-encoded string", base64String)
	}
	return secretKeyBytes
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
