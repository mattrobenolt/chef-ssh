package main

import (
	"errors"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"

	"github.com/go-chef/chef"
)

type Config struct {
	tld         string
	user        string
	serverURL   string
	userKey     string
	ipAttribute string
}

type LookupType int

const (
	LOOKUP_NODE LookupType = 1
	LOOKUP_ROLE LookupType = 2
)

func configFromEnv() *Config {
	c := &Config{
		tld:         os.Getenv("CHEF_TLD"),
		user:        os.Getenv("CHEF_USER"),
		serverURL:   os.Getenv("CHEF_SERVER_URL"),
		userKey:     os.Getenv("CHEF_USER_KEY"),
		ipAttribute: os.Getenv("CHEF_IP_ATTRIBUTE"),
	}

	if c.tld == "" {
		c.tld = ".chef"
	}
	if c.user == "" {
		u, err := user.Current()
		if err != nil {
			c.user = os.Getenv("USER")
		} else {
			c.user = u.Username
		}
	}
	if c.userKey == "" {
		c.userKey = "~/.chef/" + c.user + ".pem"
	}

	return c
}

func formatURL(url *url.URL) string {
	if url.User == nil || url.User.Username() == "" {
		return url.Host
	}
	return url.User.Username() + "@" + url.Host
}

func joinHostPort(host, port string) string {
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

func ssh(argv []string) {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		log.Fatal("Cannot find `ssh` binary.")
	}
	if err := syscall.Exec(bin, append([]string{"ssh"}, argv...), os.Environ()); err != nil {
		log.Fatal("Couldn't execute ssh binary")
	}
}

func getLookupType(hostname string) (LookupType, error) {
	if strings.HasSuffix(hostname, ".role") {
		return LOOKUP_ROLE, nil
	}
	if strings.HasSuffix(hostname, ".node") {
		return LOOKUP_NODE, nil
	}
	return -1, errors.New("Invalid lookup.")
}

func main() {
	args := os.Args[1:]
	hostname := ""
	hostArg := 0

	// Find the first non `-` argument, and
	// treat it as the destination hostname
	for i, arg := range args {
		if arg[:1] != "-" {
			hostname = arg
			hostArg = i
			break
		}
	}

	// We didn't find a hostname, so just proxy blindly
	if hostname == "" {
		ssh(args)
	}

	config := configFromEnv()

	// We couldn't parse the hostname, so it likely wasn't valid
	dst, err := url.Parse("ssh://" + hostname)
	if err != nil {
		ssh(args)
	}

	hostname, port, err := net.SplitHostPort(dst.Host)
	if err != nil {
		hostname = dst.Host
		port = ""
	}

	if !strings.HasSuffix(hostname, config.tld) {
		ssh(args)
	}

	// Beyond this point, we can assume we're destined to hit Chef server
	// and we will no longer proxy failures, we'll bubble them up.

	// strip off the TLD suffix
	hostname = hostname[:len(hostname)-len(config.tld)]

	key, err := ioutil.ReadFile(config.userKey)
	if err != nil {
		log.Fatal("Couldn't read pem file: ", err)
	}

	client, err := chef.NewClient(&chef.Config{
		Name:    config.user,
		Key:     string(key),
		BaseURL: config.serverURL + "/",
	})

	if err != nil {
		log.Fatal("Issue setting up client: ", err)
	}

	lookup, err := getLookupType(hostname)
	if err != nil {
		log.Fatal(err)
	}

	switch lookup {
	case LOOKUP_NODE:
		hostname = hostname[:len(hostname)-len(".node")]
		node, err := client.Nodes.Get(hostname)
		if err != nil {
			log.Fatal("Couldn't get node: ", err)
		}
		ip, ok := node.NormalAttributes[config.ipAttribute].(string)
		if !ok {
			log.Fatal("No ip address could be found for node.")
		}
		hostname = ip
	case LOOKUP_ROLE:
		hostname = hostname[:len(hostname)-len(".role")]
		query, err := client.Search.NewQuery("node", "role:"+hostname)
		if err != nil {
			log.Fatal("Couldn't get role: ", err)
		}
		query.Rows = 1
		query.SortBy = "name"
		result, err := query.Do(client)
		if err != nil {
			log.Fatal("Couldn't get role: ", err)
		}
		if result.Total == 0 {
			log.Fatal("Couldn't find node in role.")
		}
		ip, ok := result.Rows[0].(map[string]interface{})["normal"].(map[string]interface{})[config.ipAttribute].(string)
		if !ok {
			log.Fatal("No ip address could be found for node.")
		}
		hostname = ip
	}

	// Replace the IP address we've just fetched, and substitute
	// it inside our original request hostname to pass along to exec(3)
	dst.Host = joinHostPort(hostname, port)
	args[hostArg] = formatURL(dst)

	// Make magic happen.
	ssh(args)
}
