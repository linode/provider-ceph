package bucket

import "context"

// Not implemented. This method was added to the ExternalClient interface in crossplane v1.17.
func (c *external) Disconnect(ctx context.Context) error {
	return nil
}
