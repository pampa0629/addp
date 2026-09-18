package falkor

import "context"

// Health verifies the actual graph module and its required bounded-query
// contract without creating a probe graph or touching a generation.
func (c *Client) Health(ctx context.Context) error {
	for _, key := range []string{"TIMEOUT_MAX", "TIMEOUT_DEFAULT"} {
		value, err := c.command(ctx, "GRAPH.CONFIG", "GET", key)
		if err != nil {
			return err
		}
		if !validTimeoutConfig(value, key) {
			return ErrProtocol
		}
	}
	return nil
}

func validTimeoutConfig(value any, key string) bool {
	pair, ok := value.([]any)
	if !ok || len(pair) != 2 || pair[0] != key {
		return false
	}
	n, ok := pair[1].(int64)
	return ok && n > 0 && n <= MaxQueryTime.Milliseconds()
}
