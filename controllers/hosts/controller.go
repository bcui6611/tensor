package hosts

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
	"encoding/json"
	"bitbucket.pearson.com/apseng/tensor/controllers/metadata"
	"bitbucket.pearson.com/apseng/tensor/controllers/helpers"
	"github.com/gin-gonic/gin/binding"
)

const _CTX_HOST = "host"
const _CTX_USER = "user"
const _CTX_HOST_ID = "host_id"

// Middleware takes host_id parameter from gin.Context and
// fetches host data from the database
// it set host data under key host in gin.Context
func Middleware(c *gin.Context) {
	ID, err := util.GetIdParam(_CTX_HOST_ID, c)

	if err != nil {
		log.Print("Error while getting the Host:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		c.Abort()
		return
	}

	var h models.Host
	if err := db.Hosts().FindId(bson.ObjectIdHex(ID)).One(&h); err != nil {
		log.Print("Error while getting the Host:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Not Found"},
		})
		c.Abort()
		return
	}

	c.Set(_CTX_HOST, h)
	c.Next()
}

// GetHost returns the hsot as a JSON object
func GetHost(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)
	metadata.HostMetadata(&host)

	// send response with JSON rendered data
	c.JSON(http.StatusOK, host)
}


// GetHosts returns a JSON array of projects
func GetHosts(c *gin.Context) {

	parser := util.NewQueryParser(c)

	match := bson.M{}
	match = parser.Match([]string{"enabled", "has_active_failures", }, match)
	match = parser.Lookups([]string{"name", "description"}, match)

	//prepare the query
	query := db.Hosts().Find(match)
	if order := parser.OrderBy(); order != "" {
		query.Sort(order)
	}

	var hosts []models.Host
	// new mongodb iterator
	iter := query.Iter()
	// loop through each result and modify for our needs
	var tmpHost models.Host
	// iterate over all and only get valid objects
	for iter.Next(&tmpHost) {
		if err := metadata.HostMetadata(&tmpHost); err != nil {
			log.Println("Error while setting metatdata:", err)
			c.JSON(http.StatusInternalServerError, models.Error{
				Code:http.StatusInternalServerError,
				Messages: []string{"Error while getting Hosts"},
			})
			return
		}
		// good to go add to list
		hosts = append(hosts, tmpHost)
	}
	if err := iter.Close(); err != nil {
		log.Println("Error while retriving Host data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Hosts"},
		})
		return
	}

	count := len(hosts)
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
		Results: hosts[pgi.Skip():pgi.End()],
	})
}

// AddHost creates a new project
func AddHost(c *gin.Context) {
	var req models.Host
	user := c.MustGet(_CTX_USER).(models.User)

	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
		return
	}

	// if the host exist in the collection it is not unique
	if helpers.IsNotUniqueHost(req.Name, req.InventoryID) {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Host with this Name and Inventory already exists."},
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
	if req.GroupID != nil {
		if !helpers.GroupExist(*req.GroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Group does not exists."},
			})
			return
		}
	}

	req.ID = bson.NewObjectId()
	req.Created = time.Now()
	req.Modified = time.Now()
	req.CreatedByID = user.ID
	req.ModifiedByID = user.ID

	if err := db.Hosts().Insert(req); err != nil {
		log.Println("Error while creating Host:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Host"},
		})
		return
	}


	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Host " + req.Name + " created")

	if err := metadata.HostMetadata(&req); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while creating Host"},
		})
		return
	}

	c.JSON(http.StatusCreated, req)
}
// update will update a Host
// from request values
func UpdateHost(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.Host

	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
	}

	// check whether the inventory exist or not
	if !helpers.InventoryExist(req.InventoryID) {
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: []string{"Inventory does not exists."},
		})
		return
	}

	if req.Name != host.Name {
		// if the host exist in the collection it is not unique
		if helpers.IsNotUniqueHost(req.Name, req.InventoryID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Host with this Name and Inventory already exists."},
			})
			return
		}
	}

	// check whether the group exist or not
	if req.GroupID != nil {
		if !helpers.GroupExist(*req.GroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Group does not exists."},
			})
			return
		}
	}

	req.Created = host.Created
	req.Modified = host.Modified
	req.CreatedByID = user.ID
	req.ModifiedByID = user.ID

	//update object
	if err := db.Hosts().UpdateId(host.ID, req); err != nil {
		log.Println("Error while updating Host:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Host"},
		})
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Host " + req.Name + " updated")

	if err := metadata.HostMetadata(&req); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Host Information"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, req)
}

// Patch will patch a Host
// from request values
func PatchHost(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.PatchHost

	if err := binding.JSON.Bind(c.Request, &req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Messages: util.GetValidationErrors(err),
		})
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

	if len(req.Name) > 0 && req.Name != host.Name {
		invID := host.ID
		if len(req.InventoryID) == 12 {
			invID = req.InventoryID
		}
		// if the host exist in the collection it is not unique
		if helpers.IsNotUniqueHost(req.Name, invID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Host with this Name and Inventory already exists."},
			})
			return
		}
	}

	// check whether the group exist or not
	if req.GroupID != nil {
		if !helpers.GroupExist(*req.GroupID) {
			c.JSON(http.StatusBadRequest, models.Error{
				Code:http.StatusBadRequest,
				Messages: []string{"Group does not exists."},
			})
			return
		}
	}

	req.Modified = host.Modified
	req.ModifiedByID = user.ID

	//update object
	if err := db.Hosts().UpdateId(host.ID, bson.M{"$set": req}); err != nil {
		log.Println("Error while updating Host:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while updating Host"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(host.ID, user.ID, "Host " + req.Name + " updated")

	// get newly updated host
	var resp models.Host
	if err := db.Hosts().FindId(host.ID).One(&resp); err != nil {
		log.Print("Error while getting the updated Host:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Messages: []string{"Error while getting the updated Host"},
		})
		return
	}

	// set `related` and `summary` feilds
	if err := metadata.HostMetadata(&resp); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while getting Host Information"},
		})
		return
	}

	// send response with JSON rendered data
	c.JSON(http.StatusOK, resp)
}

func RemoveHost(c *gin.Context) {
	// get Host from the gin.Context
	host := c.MustGet(_CTX_HOST).(models.Host)
	// get user from the gin.Context
	user := c.MustGet(_CTX_USER).(models.User)

	if err := db.Hosts().RemoveId(host.ID); err != nil {
		log.Println("Error while removing Host:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while removing Host"},
		})
		return
	}

	// add new activity to activity stream
	addActivity(host.ID, user.ID, "Group " + host.Name + " deleted")

	c.AbortWithStatus(204)
}

func VariableData(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)

	variables := gin.H{}

	if err := json.Unmarshal([]byte(host.Variables), &variables); err != nil {
		log.Println("Error while getting Host variables")
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError,
			"message": []string{"Error while getting Host variables"},
		})
		return
	}

	c.JSON(http.StatusOK, variables)

}

// TODO: not implemented
func Groups(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)

	var group models.Group

	if host.GroupID != nil {
		// find group for the host
		if err := db.Groups().FindId(host.GroupID).One(&group); err != nil {
			log.Println("Error while getting groups")
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": http.StatusInternalServerError,
				"message": []string{"Error while getting groups"},
			})
			return
		}

		// set group metadata
		metadata.GroupMetadata(&group)

		// send response with JSON rendered data
		c.JSON(http.StatusOK, group)
		return
	}

	// no assigned groups
	// send response with JSON rendered data
	c.JSON(http.StatusOK, nil)
}

// TODO: not implemented
func AllGroups(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)

	var outobjects []models.Group
	var group models.Group

	if host.GroupID != nil {
		// find group for the host
		if err := db.Groups().FindId(host.GroupID).One(&group); err != nil {
			log.Println("Error while getting groups")
			c.JSON(http.StatusInternalServerError, models.Error{
				Code: http.StatusInternalServerError,
				Messages: []string{"Error while getting groups"},
			})
			return
		}

		// set group metadata
		metadata.GroupMetadata(&group)
		//add group to outobjects
		outobjects = append(outobjects, group)
		// clean object
		group = models.Group{}

		for group.ParentGroupID != nil {
			// find group for the host
			if err := db.Groups().FindId(host.GroupID).One(&group); err != nil {
				log.Println("Error while getting groups")
				c.JSON(http.StatusInternalServerError, models.Error{
					Code: http.StatusInternalServerError,
					Messages: []string{"Error while getting groups"},
				})
				return
			}

			// set group metadata
			metadata.GroupMetadata(&group)
			//add group to outobjects
			outobjects = append(outobjects, group)

			// clean object
			group = models.Group{}
		}

		nobj := len(outobjects)

		// initialize Pagination
		pgi := util.NewPagination(c, nobj)

		//if page is incorrect return 404
		if pgi.HasPage() {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Invalid page " + strconv.Itoa(pgi.Page()) + ": That page contains no results."})
			return
		}

		// send response with JSON rendered data
		c.JSON(http.StatusOK, models.Response{
			Count: nobj,
			Next: pgi.NextPage(),
			Previous: pgi.PreviousPage(),
			Results: outobjects[pgi.Limit():pgi.Offset()],
		})

		return
	}

	// no assigned groups
	// send response with JSON rendered data
	c.JSON(http.StatusOK, models.Response{
		Count: 0,
	})
}

// TODO: not complete
func ActivityStream(c *gin.Context) {
	host := c.MustGet(_CTX_HOST).(models.Host)

	var activities []models.Activity
	err := db.ActivityStream().Find(bson.M{"object_id": host.ID, "type": _CTX_HOST}).All(&activities)

	if err != nil {
		log.Println("Error while retriving Activity data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Messages: []string{"Error while Activities"},
		})
		return
	}

	count := len(activities)
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
		Results: activities[pgi.Skip():pgi.End()],
	})
}