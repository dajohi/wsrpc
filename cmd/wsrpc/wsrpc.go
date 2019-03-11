// Copyright (c) 2019 Josh Rickmar
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"

	"github.com/jrick/wsrpc"
)

const sockEnv = "WSRPCAGENT_SOCK"
const authEnv = "WSRPCAGENT_AUTH"

var (
	fs       = flag.NewFlagSet("", flag.ExitOnError)
	cFlag    = fs.String("c", "", "Root certificate PEM file")
	userFlag = fs.String("u", "", "User")
	passFlag = fs.String("p", "", "Password")
)

func main() {
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: wsrpc address [flags] method [arg]")
		fs.PrintDefaults()
		os.Exit(2)
	}
	if len(os.Args) < 2 {
		fs.Usage()
	}
	addr := os.Args[1]
	fs.Parse(os.Args[2:])
	n := fs.NArg()
	if n != 1 && n != 2 { // Expect method and optionally a JSON array arg
		fs.Usage()
	}
	method, arg := fs.Arg(0), ""
	if n == 2 {
		arg = fs.Arg(1)
		if arg != "" && arg[0] != '[' {
			log.Fatal("parameter must be JSON array")
		}
	}
	ctx := context.Background()

	var tc *tls.Config
	var pem []byte
	if *cFlag != "" {
		var err error
		pem, err = ioutil.ReadFile(*cFlag)
		if err != nil {
			log.Fatal(err)
		}
		tc = &tls.Config{RootCAs: x509.NewCertPool()}
		if !tc.RootCAs.AppendCertsFromPEM(pem) {
			log.Fatal("unparsable root certificate chain")
		}
	}

	sock, auth := os.Getenv(sockEnv), os.Getenv(authEnv)
	if sock != "" || auth != "" {
		conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
		if err != nil {
			log.Fatal(err)
		}
		err = runAgent(ctx, conn, auth, &agentArgs{
			Address:  addr,
			RootCert: string(pem),
			User:     *userFlag,
			Pass:     *passFlag,
			Method:   method,
			Params:   arg,
		})
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	c, err := wsrpc.Dial(ctx, addr, wsrpc.WithTLSConfig(tc), wsrpc.WithBasicAuth(*userFlag, *passFlag))
	if err != nil {
		log.Fatal(err)
	}
	if err := run(ctx, c, method, arg); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, c *wsrpc.Client, method string, arg string) error {
	defer c.Close()
	var args []interface{}
	if arg != "" {
		if err := json.NewDecoder(strings.NewReader(arg)).Decode(&args); err != nil {
			return err
		}
	}
	var res json.RawMessage
	if err := c.Call(ctx, method, &res, args...); err != nil {
		return err
	}
	return pp(res)
}

func pp(res json.RawMessage) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

type agentArgs struct {
	Address  string
	RootCert string
	User     string
	Pass     string
	Method   string
	Params   string
}

func runAgent(ctx context.Context, conn net.Conn, auth string, args *agentArgs) error {
	defer conn.Close()
	enc := json.NewEncoder(conn)
	if err := enc.Encode(auth); err != nil {
		log.Fatal(err)
	}
	if err := enc.Encode(args); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	var es string
	if err := dec.Decode(&es); err != nil {
		return err
	}
	if es != "" {
		log.Fatal(es)
	}
	var res json.RawMessage
	if err := dec.Decode(&res); err != nil {
		return err
	}
	return pp(res)
}
