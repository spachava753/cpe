package agent

import "testing"

func Test_escapeJsonString(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "json tool call",
			args: args{
				s: `{"name":"shell","parameters":{"command":"cd /home/shashank/working/dev/go-projects/cpe \u0026\u0026 pwd \u0026\u0026 ls -la"}}`,
			},
			want: `{"name":"shell","parameters":{"command":"cd /home/shashank/working/dev/go-projects/cpe && pwd && ls -la"}}`,
		},
		{
			name: "pipes",
			args: args{
				s: `{"command":"cat file \u007c\u007c echo fail"}`,
			},
			want: `{"command":"cat file || echo fail"}`,
		},
		{
			name: "quotes",
			args: args{
				s: `{"message":"He said \u0022hello\u0022"}`,
			},
			want: `{"message":"He said \"hello\""}`,
		},
		{
			name: "mixed escapes",
			args: args{
				s: `{"cmd":"grep \u0022pattern\u0022 file \u007c\u007c exit 1"}`,
			},
			want: `{"cmd":"grep \"pattern\" file || exit 1"}`,
		},
		{
			name: "no escapes",
			args: args{
				s: `{"simple":"text"}`,
			},
			want: `{"simple":"text"}`,
		},
		{
			name: "semicolon and colon",
			args: args{
				s: `{"cmd":"echo test\u003b ls \u003a\u003a"}`,
			},
			want: `{"cmd":"echo test; ls ::"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeJsonString(tt.args.s); got != tt.want {
				t.Errorf("unescapeJsonString() = %v, want %v", got, tt.want)
			}
		})
	}
}
