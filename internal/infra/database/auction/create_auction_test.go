package auction

import (
	"context"
	"os"
	"testing"
	"time"

	"fullcycle-auction_go/internal/entity/auction_entity"

	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

// TestCreateAuction_ClosesAfterDuration proves the scenario described in the
// requirements:
//
//	1. Create an auction        -> starts Active
//	2. Wait AUCTION_DURATION    -> background goroutine fires
//	3. Check the auction status -> automatically updated to Completed (Closed)
//
// It uses a mocked Mongo deployment so no live database is required, and
// inspects the commands the driver actually sent to assert the background
// goroutine issued an update setting the status to Completed.
func TestCreateAuction_ClosesAfterDuration(t *testing.T) {
	os.Setenv("AUCTION_DURATION", "200ms")
	defer os.Unsetenv("AUCTION_DURATION")

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("auction is closed automatically once the duration expires", func(mt *mtest.T) {
		// One response for the initial insert, one for the update the
		// background goroutine performs when the auction expires.
		mt.AddMockResponses(
			mtest.CreateSuccessResponse(),
			mtest.CreateSuccessResponse(),
		)

		repo := &AuctionRepository{Collection: mt.Coll}

		auctionEntity, err := auction_entity.CreateAuction(
			"Playstation 5", "Games", "A brand new console", auction_entity.New)
		if err != nil {
			mt.Fatalf("unexpected error building auction: %s", err.Message)
		}

		if createErr := repo.CreateAuction(context.Background(), auctionEntity); createErr != nil {
			mt.Fatalf("unexpected error creating auction: %s", createErr.Message)
		}

		if auctionEntity.Status != auction_entity.Active {
			mt.Fatalf("auction should start Active, got %v", auctionEntity.Status)
		}

		// Wait comfortably past the configured duration so the goroutine runs.
		time.Sleep(600 * time.Millisecond)

		var (
			updateSeen   bool
			closedStatus int32 = -1
			targetID     string
		)
		for _, ev := range mt.GetAllStartedEvents() {
			if ev.CommandName != "update" {
				continue
			}
			updateSeen = true

			updates := ev.Command.Lookup("updates").Array()
			first, arrErr := updates.IndexErr(0)
			if arrErr != nil {
				mt.Fatalf("update command had no updates: %v", arrErr)
			}
			updateDoc := first.Value().Document()
			targetID = updateDoc.Lookup("q").Document().Lookup("_id").StringValue()
			closedStatus = updateDoc.Lookup("u").Document().
				Lookup("$set").Document().Lookup("status").Int32()
		}

		if !updateSeen {
			mt.Fatal("expected the background goroutine to close the auction, but no update was sent")
		}
		if targetID != auctionEntity.Id {
			mt.Fatalf("close update targeted %q, want auction id %q", targetID, auctionEntity.Id)
		}
		if closedStatus != int32(auction_entity.Completed) {
			mt.Fatalf("auction status set to %d, want Completed (%d)", closedStatus, auction_entity.Completed)
		}
	})
}
