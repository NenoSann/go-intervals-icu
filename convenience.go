package intervalsicu

import "context"

// GetAthleteInfo fetches the default athlete configured on the client.
func (c *Client) GetAthleteInfo(ctx context.Context) (*GetAthleteResult, error) {
	return c.GetAthlete(ctx, GetAthleteParams{})
}

// UpdateAthleteInfo updates the default athlete configured on the client.
func (c *Client) UpdateAthleteInfo(ctx context.Context, body AthleteUpdateDTO) (*UpdateAthleteResult, error) {
	return c.UpdateAthlete(ctx, UpdateAthleteParams{Body: body})
}
