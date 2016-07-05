package models

import (
	"gopkg.in/mgo.v2/bson"
	database "github.com/gamunu/hilbertspace/db"
)

// Inventory is the model for
// project_inventory collection
type Inventory struct {
	ID        bson.ObjectId    `bson:"_id" json:"id"`
	Name      string `bson:"name" json:"name" binding:"required"`
	ProjectID bson.ObjectId    `bson:"project_id" json:"project_id"`
	Inventory []string `bson:"inventory" json:"inventory"`

	// accesses dynamic inventory
	KeyID     bson.ObjectId      `bson:"key_id" json:"key_id"`
	// accesses hosts in inventory
	SshKeyID  bson.ObjectId      `bson:"ssh_key_id" json:"ssh_key_id"`
	// static/aws/do/gcloud
	Type      string `bson:"type" json:"type"`

	SshKey    AccessKey `bson:"-" json:"-"`
	Key       AccessKey `bson:"-" json:"-"`
}

func (inv Inventory) Insert() error {
	c := database.MongoDb.C("project_inventory")
	return c.Insert(inv)
}

func (inv Inventory) Remove() error {
	c := database.MongoDb.C("project_inventory")
	return c.RemoveId(inv.ID)
}

func (inv Inventory) Update() error {
	c := database.MongoDb.C("project_inventory")
	return c.UpdateId(inv.ID, inv)
}