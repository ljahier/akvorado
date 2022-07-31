// SPDX-FileCopyrightText: 2022 Free Mobile
// SPDX-License-Identifier: AGPL-3.0-only

package helpers_test

import (
	"net"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v2"

	"akvorado/common/helpers"
)

func TestSubnetMapUnmarshalHook(t *testing.T) {
	var nilMap map[string]string
	cases := []struct {
		Description string
		Input       interface{}
		Tests       map[string]string
		Error       bool
		YAML        interface{}
	}{
		{
			Description: "nil",
			Input:       nilMap,
			Tests: map[string]string{
				"203.0.113.1": "",
			},
		}, {
			Description: "empty",
			Input:       gin.H{},
			Tests: map[string]string{
				"203.0.113.1": "",
			},
		}, {
			Description: "IPv4",
			Input:       gin.H{"203.0.113.0/24": "customer1"},
			Tests: map[string]string{
				"::ffff:203.0.113.18": "customer1",
				"203.0.113.16":        "customer1",
				"203.0.1.1":           "",
				"::ffff:203.0.1.1":    "",
				"2001:db8:1::12":      "",
			},
		}, {
			Description: "IPv6",
			Input:       gin.H{"2001:db8:1::/64": "customer2"},
			Tests: map[string]string{
				"2001:db8:1::1": "customer2",
				"2001:db8:1::2": "customer2",
				"2001:db8:2::2": "",
			},
		}, {
			Description: "Invalid subnet (1)",
			Input:       gin.H{"192.0.2.1/38": "customer"},
			Error:       true,
		}, {
			Description: "Invalid subnet (2)",
			Input:       gin.H{"192.0.2.1/255.0.255.0": "customer"},
			Error:       true,
		}, {
			Description: "Invalid subnet (3)",
			Input:       gin.H{"2001:db8::/1000": "customer"},
			Error:       true,
		}, {
			Description: "Single value",
			Input:       "customer",
			Tests: map[string]string{
				"203.0.113.4": "customer",
				"2001:db8::1": "customer",
			},
			YAML: map[string]string{
				"::/0": "customer",
			},
		},
	}
	for _, tc := range cases {
		if tc.YAML == nil {
			if tc.Error {
				tc.YAML = map[string]string{}
			} else {
				tc.YAML = tc.Input
			}
		}
		t.Run(tc.Description, func(t *testing.T) {
			var tree helpers.SubnetMap[string]
			decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
				Result:      &tree,
				ErrorUnused: true,
				Metadata:    nil,
				DecodeHook:  helpers.SubnetMapUnmarshallerHook[string](),
			})
			if err != nil {
				t.Fatalf("NewDecoder() error:\n%+v", err)
			}
			err = decoder.Decode(tc.Input)
			if err != nil && !tc.Error {
				t.Fatalf("Decode() error:\n%+v", err)
			} else if err == nil && tc.Error {
				t.Fatal("Decode() did not return an error")
			}
			got := map[string]string{}
			for k := range tc.Tests {
				v, _ := tree.Lookup(net.ParseIP(k))
				got[k] = v
			}
			if diff := helpers.Diff(got, tc.Tests); diff != "" {
				t.Fatalf("Decode() (-got, +want):\n%s", diff)
			}

			// Try to unmarshal with YAML
			buf, err := yaml.Marshal(tree)
			if err != nil {
				t.Fatalf("yaml.Marshal() error:\n%+v", err)
			}
			got = map[string]string{}
			if err := yaml.Unmarshal(buf, &got); err != nil {
				t.Fatalf("yaml.Unmarshal() error:\n%+v", err)
			}
			if diff := helpers.Diff(got, tc.YAML); diff != "" {
				t.Fatalf("MarshalYAML() (-got, +want):\n%s", diff)
			}
		})
	}
}
