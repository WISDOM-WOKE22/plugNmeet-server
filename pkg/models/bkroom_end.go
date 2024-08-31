package models

import (
	"github.com/mynaparrot/plugnmeet-protocol/plugnmeet"
	"github.com/mynaparrot/plugnmeet-server/pkg/config"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"time"
)

func (m *BreakoutRoomModel) EndBreakoutRoom(r *plugnmeet.EndBreakoutRoomReq) error {
	_, err := m.fetchBreakoutRoom(r.RoomId, r.BreakoutRoomId)
	if err != nil {
		return err
	}
	_, _ = m.rm.EndRoom(&plugnmeet.RoomEndReq{RoomId: r.BreakoutRoomId})

	_ = m.rs.DeleteBreakoutRoom(r.RoomId, r.BreakoutRoomId)
	_ = m.performPostHookTask(r.RoomId)
	return nil
}

func (m *BreakoutRoomModel) EndBreakoutRooms(roomId string) error {
	rooms, err := m.fetchBreakoutRooms(roomId)
	if err != nil {
		return err
	}

	for _, r := range rooms {
		_ = m.EndBreakoutRoom(&plugnmeet.EndBreakoutRoomReq{
			BreakoutRoomId: r.Id,
			RoomId:         roomId,
		})
	}
	return nil
}

func (m *BreakoutRoomModel) PostTaskAfterRoomStartWebhook(roomId string, metadata *plugnmeet.RoomMetadata) error {
	// now in livekit rooms are created almost instantly & sending webhook response
	// if this happened then we'll have to wait few seconds otherwise room info can't be found
	time.Sleep(config.WaitBeforeBreakoutRoomOnAfterRoomStart)

	room, err := m.fetchBreakoutRoom(metadata.ParentRoomId, roomId)
	if err != nil {
		return err
	}
	room.Created = metadata.StartedAt
	room.Started = true

	marshal, err := protojson.Marshal(room)
	if err != nil {
		return err
	}

	val := map[string]string{
		roomId: string(marshal),
	}
	err = m.rs.InsertOrUpdateBreakoutRoom(metadata.ParentRoomId, val)
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func (m *BreakoutRoomModel) PostTaskAfterRoomEndWebhook(roomId, metadata string) error {
	if metadata == "" {
		return nil
	}
	meta, err := m.natsService.UnmarshalRoomMetadata(metadata)
	if err != nil {
		return err
	}

	if meta.IsBreakoutRoom {
		_ = m.rs.DeleteBreakoutRoom(meta.ParentRoomId, roomId)
		_ = m.performPostHookTask(meta.ParentRoomId)
	} else {
		err = m.EndBreakoutRooms(roomId)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *BreakoutRoomModel) performPostHookTask(roomId string) error {
	c, err := m.rs.CountBreakoutRooms(roomId)
	if err != nil {
		log.Error(err)
		return err
	}

	if c != 0 {
		return nil
	}

	// no room left so, delete breakoutRoomKey key for this room
	_ = m.rs.DeleteAllBreakoutRoomsByParentRoomId(roomId)

	// if no rooms left, then we can update metadata
	meta, err := m.natsService.GetRoomMetadataStruct(roomId)
	if err != nil {
		return err
	}
	if meta == nil {
		// indicating room was ended
		return nil
	}

	meta.RoomFeatures.BreakoutRoomFeatures.IsActive = false
	err = m.natsService.UpdateAndBroadcastRoomMetadata(roomId, meta)
	if err != nil {
		return err
	}

	return nil
}
