package tcp

import "github.com/hashicorp/boundary/internal/target"

func init() {
	target.Register(typeName, rh)
}
