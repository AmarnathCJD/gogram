package telegram

type GroupCallMedia struct {
	Peer        Peer
	started     bool
	currentFile string
}

func (c *Client) StartGroupCallMedia(peer interface{}) (*GroupCallMedia, error) {
	peerDialog, err := c.GetSendablePeer(peer)
	if err != nil {
		return nil, err
	}
	// Start the group call
	_, e := c.PhoneCreateGroupCall(&PhoneCreateGroupCallParams{
		Peer:     peerDialog,
		RandomID: int32(GenRandInt()),
	})
	if e != nil {
		return nil, e
	}
	// TODO : Check if the group call is already started
	// TODO : Implement this
	return &GroupCallMedia{}, nil
}
