// Copyright (c) Mainflux
// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/mainflux/mainflux"
	"github.com/mainflux/mainflux/readers"
	preader "github.com/mainflux/mainflux/readers/postgres"
	pwriter "github.com/mainflux/mainflux/writers/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	subtopic    = "subtopic"
	msgsNum     = 42
	valueFields = 5
)

func TestMessageReadAll(t *testing.T) {
	messageRepo := pwriter.New(db)

	chanID, err := uuid.NewV4()
	require.Nil(t, err, fmt.Sprintf("got unexpected error: %s", err))
	pubID, err := uuid.NewV4()
	require.Nil(t, err, fmt.Sprintf("got unexpected error: %s", err))
	wrongID, err := uuid.NewV4()
	require.Nil(t, err, fmt.Sprintf("got unexpected error: %s", err))

	msg := mainflux.Message{
		Channel:   chanID.String(),
		Publisher: pubID.String(),
		Protocol:  "mqtt",
	}

	messages := []mainflux.Message{}
	subtopicMsgs := []mainflux.Message{}
	now := time.Now().Unix()
	for i := 0; i < msgsNum; i++ {
		// Mix possible values as well as value sum.
		count := i % valueFields
		msg.Subtopic = ""
		switch count {
		case 0:
			msg.Subtopic = subtopic
			msg.Value = &mainflux.Message_FloatValue{FloatValue: 5}
		case 1:
			msg.Value = &mainflux.Message_BoolValue{BoolValue: false}
		case 2:
			msg.Value = &mainflux.Message_StringValue{StringValue: "value"}
		case 3:
			msg.Value = &mainflux.Message_DataValue{DataValue: "base64data"}
		case 5:
			msg.ValueSum = &mainflux.SumValue{Value: 45}
		}
		msg.Time = float64(now - int64(i))

		err := messageRepo.Save(msg)
		assert.Nil(t, err, fmt.Sprintf("expected no error got %s\n", err))
		messages = append(messages, msg)
		if count == 0 {
			subtopicMsgs = append(subtopicMsgs, msg)
		}
	}

	reader := preader.New(db)

	// Since messages are not saved in natural order,
	// cases that return subset of messages are only
	// checking data result set size, but not content.
	cases := map[string]struct {
		chanID string
		offset uint64
		limit  uint64
		query  map[string]string
		page   readers.MessagesPage
	}{
		"read message page for existing channel": {
			chanID: chanID.String(),
			offset: 0,
			limit:  msgsNum,
			page: readers.MessagesPage{
				Total:    msgsNum,
				Offset:   0,
				Limit:    msgsNum,
				Messages: messages,
			},
		},
		"read message page for non-existent channel": {
			chanID: wrongID.String(),
			offset: 0,
			limit:  msgsNum,
			page: readers.MessagesPage{
				Total:    0,
				Offset:   0,
				Limit:    msgsNum,
				Messages: []mainflux.Message{},
			},
		},
		"read message last page": {
			chanID: chanID.String(),
			offset: 40,
			limit:  5,
			page: readers.MessagesPage{
				Total:    msgsNum,
				Offset:   40,
				Limit:    5,
				Messages: messages[40:42],
			},
		},
		"read message with non-existent subtopic": {
			chanID: chanID.String(),
			offset: 0,
			limit:  msgsNum,
			query:  map[string]string{"subtopic": "not-present"},
			page: readers.MessagesPage{
				Total:    0,
				Offset:   0,
				Limit:    msgsNum,
				Messages: []mainflux.Message{},
			},
		},
		"read message with subtopic": {
			chanID: chanID.String(),
			offset: 0,
			limit:  uint64(len(subtopicMsgs)),
			query:  map[string]string{"subtopic": subtopic},
			page: readers.MessagesPage{
				Total:    uint64(len(subtopicMsgs)),
				Offset:   0,
				Limit:    uint64(len(subtopicMsgs)),
				Messages: subtopicMsgs,
			},
		},
		"read message with publisher/protocols": {
			chanID: chanID.String(),
			offset: 0,
			limit:  msgsNum,
			query:  map[string]string{"publisher": pubID.String(), "protocol": "mqtt"},
			page: readers.MessagesPage{
				Total:    msgsNum,
				Offset:   0,
				Limit:    msgsNum,
				Messages: messages,
			},
		},
	}

	for desc, tc := range cases {
		result, err := reader.ReadAll(tc.chanID, tc.offset, tc.limit, tc.query)
		assert.Nil(t, err, fmt.Sprintf("%s: expected no error got %s", desc, err))
		assert.ElementsMatch(t, tc.page.Messages, result.Messages, fmt.Sprintf("%s: expected %v got %v", desc, tc.page.Messages, result.Messages))
		assert.Equal(t, tc.page.Total, result.Total, fmt.Sprintf("%s: expected %v got %v", desc, tc.page.Total, result.Total))
	}
}
