package groups

import (
	"gopkg.in/mgo.v2/bson"
	"time"
	"net/http"
	"bitbucket.pearson.com/apseng/tensor/models"
	"github.com/gin-gonic/gin"
	"bitbucket.pearson.com/apseng/tensor/db"
	"log"
	"bitbucket.pearson.com/apseng/tensor/util"
	"strconv"
	"bitbucket.pearson.com/apseng/tensor/api/metadata"
	"encoding/json"
	"bitbucket.pearson.com/apseng/tensor/api/helpers"
)

const _CTX_GROUP = "group"
const _CTX_USER = "user"
const _CTX_GROUP_ID = "group_id"

// GroupMiddleware takes host_id parameter from gin.Context and
// fetches host data from the database
// it set host data under key host in gin.Context
func Middleware(c *gin.Context) {

	ID, err := util.GetIdParam(_CTX_GROUP_ID, c)

	if err != nil {
		log.Print("Error while getting the Group:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		return
	}

	var group models.Group
	err = db.Groups().FindId(bson.ObjectIdHex(ID)).One(&group);

	if err != nil {
		log.Print("Error while getting the Group:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		return
	}

	c.Set(_CTX_GROUP, group)
	c.Next()
}

// GetHost returns the host as a serialized JSON object
func GetGroup(c *gin.Context) {
	group := c.MustGet(_CTX_GROUP).(models.Group)

	if err := metadata.GroupMetadata(&group); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Group"},
		})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, group)
}

// GetGroups returns groups as a serialized JSON object
func GetGroups(c *gin.Context) {

	parser := util.NewQueryParser(c)
	match := parser.Match([]string{"source", "has_active_failures", })
	con := parser.IContains([]string{"name"});

	if con != nil {
		if match != nil {
			for i, v := range con {
				match[i] = v
			}
		} else {
			match = con
		}
	}

	query := db.Groups().Find(match) // prepare the query
	// set sort value to the query based on request parameters
	order := parser.OrderBy();
	if order != "" {
		query.Sort(order)
	}

	var groups []models.Group
	// new mongodb iterator
	iter := query.Iter()
	// loop through each result and modify for our needs
	var tmpGroup models.Group
	// iterate over all and only get valid objects
	for iter.Next(&tmpGroup) {
		if err := metadata.GroupMetadata(&tmpGroup); err != nil {
			log.Println("Error while setting metatdata:", err)
			c.JSON(http.StatusInternalServerError, models.Error{
				Code:http.StatusInternalServerError,
				Messages: []string{"Error while getting Groups"},
			})
			return
		}
		// good to go add to list
		groups = append(groups, tmpGroup)
	}
	if err := iter.Close(); err != nil {
		log.Println("Error while retriving Group data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Group"},
		})
		return
	}

	count := len(groups)
	pgi := util.NewPagination(c, count)
	//if page is incorrect return 404
	if pgi.HasPage() {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
		return
	}
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count:count,
		Next: pgi.NextPage(),
		Previous: pgi.PreviousPage(),
		Results: groups[pgi.Skip():pgi.End()],
	})
}

// AddGroup creates a new group
func AddGroup(c *gin.Context) {
	var req models.Group
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	err := c.BindJSON(&req);
	if err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// if the group exist in the collection it is not unique
	if helpers.IsNotUniqueGroup(req.Name, req.InventoryID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Group with this Name and Inventory already exists."},
		})
		return
	}

	// check whether the inventory exist or not
	if !helpers.InventoryExist(req.InventoryID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Inventory does not exists."},
		})
		return
	}

	// check whether the group exist or not
	if req.ParentGroupID != nil {
		if !helpers.ParentGroupExist(*req.ParentGroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Parent Group does not exists."},
			})
			return
		}
	}

	// create new object to omit unnecessary fields
	req.ID = bson.NewObjectId()
	req.Created = time.Now()
	req.Modified = time.Now()
	req.CreatedByID = user.ID
	req.ModifiedByID = user.ID

	err = db.Groups().Insert(req);
	if err != nil {
		log.Println("Error while creating Group:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Group"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Group " + req.Name + " created")

	err = metadata.GroupMetadata(&req);
	if err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Group"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusCreated, req)
}

// UpdateGroup will update the Group
func UpdateGroup(c *gin.Context) {
	// get Group from the gin.Context
	group := c.MustGet(_CTX_GROUP).(models.Group)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.Group
	err := c.BindJSON(&req);
	if err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// if the group exist in the collection it is not unique
	if helpers.IsUniqueGroup(req.Name, req.InventoryID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Group with this Name and Inventory does not exists."},
		})
		return
	}

	// check whether the inventory exist or not
	if !helpers.InventoryExist(req.InventoryID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Inventory does not exists."},
		})
		return
	}

	// check whether the group exist or not
	if req.ParentGroupID != nil {
		if !helpers.ParentGroupExist(*req.ParentGroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Parent Group does not exists."},
			})
			return
		}
	}

	req.Created = group.Created
	req.CreatedByID = group.CreatedByID
	req.Modified = time.Now()
	req.ModifiedByID = user.ID

	// update object
	err = db.Groups().UpdateId(group.ID, req);
	if err != nil {
		log.Println("Error while updating Group:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Group"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Group " + group.Name + " updated")

	// set `related` and `summary` feilds
	err = metadata.GroupMetadata(&req);
	if err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Group"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, req)
}


// UpdateGroup will update the Group
func PatchGroup(c *gin.Context) {
	// get Group from the gin.Context
	group := c.MustGet(_CTX_GROUP).(models.Group)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.PatchGroup
	err := c.BindJSON(&req);
	if err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// since this is a patch request if the name specified check the
	// inventory name is unique
	if len(req.Name) > 0 {
		objId := group.InventoryID
		// if inventory id specified use it otherwise use
		// old inventory id
		if len(req.InventoryID) == 12 {
			objId = req.InventoryID
		}
		// check wheather the group exist in the collection, if not fail.
		// if the group unique then it is not in the collection, abort any updates
		if helpers.IsUniqueGroup(req.Name, objId) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Group with this Name and Inventory does not exists."},
			})
			return
		}
	}

	// check whether the inventory exist or not
	if len(req.InventoryID) == 12 {
		if !helpers.InventoryExist(req.InventoryID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Inventory does not exists."},
			})
			return
		}
	}

	// check whether the group exist or not
	if req.ParentGroupID != nil {
		if !helpers.ParentGroupExist(*req.ParentGroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Parent Group does not exists."},
			})
			return
		}
	}

	req.Modified = time.Now()
	req.ModifiedByID = user.ID

	// update object
	changeinf, err := db.Hosts().UpsertId(bson.M{"_id" :group.ID}, req);
	if err != nil {
		log.Println("Error while updating Group:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Group"},
		})
		return
	}

	log.Printf("Matched: %d, Removed: %d, Updated: %d, UpsertId: %s", changeinf.Matched, changeinf.Removed, changeinf.Updated, changeinf.UpsertedId)

	// add new activity to activity stream
	addActivity(group.ID, user.ID, "Group " + group.Name + " updated")

	// get newly updated group
	var resp models.Group
	if err = db.Groups().FindId(group.ID).One(&resp); err != nil {
		log.Print("Error while getting the updated Group:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Error while getting the updated Group"},
		})
		return
	}

	// set `related` and `summary` feilds
	err = metadata.GroupMetadata(&resp);
	if err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Group Infomation"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, resp)
}

// RemoveGroup will remove the Group
// from the models._CTX_GROUP collection
func RemoveGroup(c *gin.Context) {
	// get Group from the gin.Context
	group := c.MustGet(_CTX_GROUP).(models.Group)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	var childgroups []models.Group

	//find the group and all child groups
	query := bson.M{
		"$or": []bson.M{
			{"parent_group_id": group.ID},
			{"_id": group.ID},
		},
	}
	err := db.Groups().Find(query).Select(bson.M{"_id":1}).All(&childgroups);
	if err != nil {
		log.Println("Error while getting child Groups:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Group"},
		})
		return
	}

	// get group ids
	var ids []bson.ObjectId

	for _, v := range childgroups {
		ids = append(ids, v.ID)
	}

	//remove hosts that has group ids of group and child groups
	changes, err := db.Hosts().RemoveAll(bson.M{"group_id": bson.M{"$in": ids}});
	if err != nil {
		log.Println("Error while removing Group Hosts:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Group Hosts"},
		})
		return
	}
	log.Println("Hosts remove info:", changes.Removed)

	// remove groups from the collection
	changes, err = db.Groups().RemoveAll(query);
	if err != nil {
		log.Println("Error while removing Group:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Group"},
		})
		return
	}
	log.Println("Groups remove info:", changes.Removed)

	// add new activity to activity stream
	addActivity(group.ID, user.ID, "Group " + group.Name + " deleted")

	// abort with 204 status code
	c.AbortWithStatus(http.StatusNoContent)
}

func VariableData(c *gin.Context) {
	group := c.MustGet(_CTX_GROUP).(models.Group)

	variables := gin.H{}

	if err := json.Unmarshal([]byte(group.Variables), &variables); err != nil {
		log.Println("Error while getting Group variables")
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError,
			"message": []string{"Error while getting Group variables"},
		})
		return
	}

	c.JSON(http.StatusOK, variables)
}