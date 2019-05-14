package secrethub

import (
	"github.com/secrethub/secrethub-go/internals/api"
	"github.com/secrethub/secrethub-go/internals/assert"
	"github.com/secrethub/secrethub-go/pkg/secrethub/fakeclient"
	"testing"
)

func TestNewEnv(t *testing.T) {
	cases := map[string]struct {
		tpl      map[string]string
		client   fakeclient.WithDataGetter
		expected map[string]string
		err      error
	}{
		"success": {
			tpl: map[string]string{
				"yml": "foo: bar\nbaz: ${path/to/secret}",
				"env": "foo=bar\nbaz=${path/to/secret}",
			},
			client: fakeclient.WithDataGetter{
				ReturnsVersion: &api.SecretVersion{
					Data: []byte("foobar"),
				},
			},
			expected: map[string]string{
				"foo": "bar",
				"baz": "foobar",
			},
		},
		"= in value": {
			tpl: map[string]string{
				"yml": "foo: foo=bar",
				"env": "foo=foo=bar",
			},
			expected: map[string]string{
				"foo": "foo=bar",
			},
		},
		"double ==": {
			tpl: map[string]string{
				"yml": "foo: =foobar",
				"env": "foo==foobar",
			},
			expected: map[string]string{
				"foo": "=foobar",
			},
		},
		"inject not closed": {
			tpl: map[string]string{
				"yml": "foo: ${path/to/secret",
				"env": "foo=${path/to/secret",
			},
			expected: map[string]string{
				"foo": "${path/to/secret",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := fakeclient.Client{
				SecretService: &fakeclient.SecretService{
					VersionService: &fakeclient.SecretVersionService{
						WithDataGetter: tc.client,
					},
				},
			}

			for format, tpl := range tc.tpl {
				t.Run(format, func(t *testing.T) {
					env, err := NewEnv(tpl)
					assert.OK(t, err)

					actual, err := env.Env(client)
					assert.Equal(t, err, tc.err)

					assert.Equal(t, actual, tc.expected)
				})
			}

		})
	}
}
