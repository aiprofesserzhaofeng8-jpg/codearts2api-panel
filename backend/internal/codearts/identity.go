package codearts

// SetIdentity allows settings updates while inference requests are in flight.
func (c *Client) SetIdentity(id Identity) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	c.Identity = id
}
