// SPDX-FileCopyrightText: 2022 Free Mobile
// SPDX-License-Identifier: AGPL-3.0-only

package helpers

import (
	"fmt"
	"net"
	"reflect"

	"github.com/kentik/patricia"
	tree "github.com/kentik/patricia/generics_tree"
	"github.com/mitchellh/mapstructure"
)

// SubnetMap maps subnets to values and allow to lookup by IP address.
// Internally, everything is stored as an IPv6 (using v6-mapped IPv4
// addresses).
type SubnetMap[V any] struct {
	tree *tree.TreeV6[V]
}

// Lookup will search for the most specific subnet matching the
// provided IP address and return the value associated with it.
func (sm *SubnetMap[V]) Lookup(ip net.IP) (V, bool) {
	if sm.tree == nil {
		var value V
		return value, false
	}
	ip = ip.To16()
	ok, value := sm.tree.FindDeepestTag(patricia.NewIPv6Address(ip, 128))
	return value, ok
}

// SubnetMapUnmarshallerHook decodes SubnetMap and notably check that
// valid networks are provided as key. It also accepts a single value
// instead of a map for backward compatibility.
func SubnetMapUnmarshallerHook[V any]() mapstructure.DecodeHookFunc {
	return func(from, to reflect.Value) (interface{}, error) {
		if to.Type() != reflect.TypeOf(SubnetMap[V]{}) {
			return from.Interface(), nil
		}
		output := map[string]interface{}{}
		var zero V
		if from.Kind() == reflect.Map {
			// First case, we have a map
			iter := from.MapRange()
			for i := 0; iter.Next(); i++ {
				k := iter.Key()
				v := iter.Value()
				if k.Kind() == reflect.Interface {
					k = k.Elem()
				}
				if k.Kind() != reflect.String {
					return nil, fmt.Errorf("key %d is not a string (%s)", i, k.Kind())
				}
				// Parse key
				_, ipNet, err := net.ParseCIDR(k.String())
				if err != nil {
					return nil, err
				}
				// Convert key to IPv6
				ones, bits := ipNet.Mask.Size()
				if bits != 32 && bits != 128 {
					return nil, fmt.Errorf("key %d has an invalid netmask", i)
				}
				var key string
				if bits == 32 {
					key = fmt.Sprintf("::ffff:%s/%d", ipNet.IP.String(), ones+96)
				} else {
					key = ipNet.String()
				}

				output[key] = v.Interface()
			}
		} else if from.Type() == reflect.TypeOf(zero) || from.Type().ConvertibleTo(reflect.TypeOf(zero)) {
			// Second case, we have a single value
			output["::/0"] = from.Interface()
		} else {
			return from.Interface(), nil
		}

		// We have to decode output map, then turn it into a SubnetMap[V]
		var intermediate map[string]V
		intermediateDecoder, err := mapstructure.NewDecoder(
			GetMapStructureDecoderConfig(&intermediate))
		if err != nil {
			return nil, fmt.Errorf("cannot create subdecoder: %w", err)
		}
		if err := intermediateDecoder.Decode(output); err != nil {
			return nil, fmt.Errorf("unable to decode %q: %w", reflect.TypeOf(zero).Name(), err)
		}
		trie := tree.NewTreeV6[V]()
		for k, v := range intermediate {
			_, ipNet, err := net.ParseCIDR(k)
			if err != nil {
				// Should not happen
				return nil, err
			}
			plen, _ := ipNet.Mask.Size()
			trie.Set(patricia.NewIPv6Address(ipNet.IP.To16(), uint(plen)), v)
		}

		return SubnetMap[V]{trie}, nil
	}
}

func (sm SubnetMap[V]) MarshalYAML() (interface{}, error) {
	output := map[string]V{}
	if sm.tree == nil {
		return output, nil
	}
	iter := sm.tree.Iterate()
	for iter.Next() {
		output[iter.Address().String()] = iter.Tags()[0]
	}
	return output, nil
}
