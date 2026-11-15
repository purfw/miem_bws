package subscriptions

import "github.com/confhub/shared/openapi"

func APIModels() map[string]openapi.Models {
	created := map[string]any{"id": "", "conference_id": "", "notify_on": []string{}, "channels": []string{}, "created_at": item{}.CreatedAt, "unsubscribe_token": ""}
	return map[string]openapi.Models{
		"subscriptions.(*Handler).create":         {Request: createRequest{}, Response: map[string]any{"data": created}, Status: 201, Auth: true},
		"subscriptions.(*Handler).createCriteria": {Request: criteriaRequest{}, Response: map[string]any{"data": item{}}, Status: 201, Auth: true},
		"subscriptions.(*Handler).list":           {Response: map[string]any{"data": []item{}}, Auth: true},
		"subscriptions.(*Handler).delete":         {Status: 204, Auth: true},
		"subscriptions.(*Handler).unsubscribe":    {Status: 302},
	}
}
