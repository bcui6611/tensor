package credentials

import (
	"bitbucket.pearson.com/apseng/tensor/models"
	"github.com/gin-gonic/gin"
	"gopkg.in/mgo.v2/bson"
	"log"
	"net/http"
	"bitbucket.pearson.com/apseng/tensor/util"
	"time"
	"strconv"
	"bitbucket.pearson.com/apseng/tensor/crypt"
	"bitbucket.pearson.com/apseng/tensor/db"
	"bitbucket.pearson.com/apseng/tensor/roles"
	"bitbucket.pearson.com/apseng/tensor/api/metadata"
)

const _CTX_CREDENTIAL = "credential"
const _CTX_CREDENTIAL_ID = "credential_id"
const _CTX_USER = "user"

func Middleware(c *gin.Context) {
	ID := c.Params.ByName(_CTX_CREDENTIAL_ID)
	user := c.MustGet(_CTX_USER).(models.User)

	collection := db.C(db.CREDENTIALS)
	var credential models.Credential
	if err := collection.FindId(bson.ObjectIdHex(ID)).One(&credential); err != nil {
		log.Print("Error while getting the Credential:", err) // log error to the system log
		c.JSON(http.StatusNotFound, models.Error{
			Code:http.StatusNotFound,
			Message: "Not Found",
		})
		return
	}

	// reject the request if the user doesn't have permissions
	if roles.CredentialRead(user, credential) {
		c.JSON(http.StatusUnauthorized, models.Error{
			Code: http.StatusUnauthorized,
			Message: "Unauthorized",
		})
		return
	}

	c.Set(_CTX_CREDENTIAL, credential)
	c.Next()
}

// GetProject returns the project as a JSON object
func GetCredential(c *gin.Context) {
	credential := c.MustGet(_CTX_CREDENTIAL).(models.Credential)

	hideEncrypted(&credential)

	if err := metadata.CredentialMetadata(&credential); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while getting Credential",
		})
		return
	}

	c.JSON(http.StatusOK, credential)
}

func GetCredentials(c *gin.Context) {
	user := c.MustGet(_CTX_USER).(models.User)

	dbc := db.C(db.CREDENTIALS)

	parser := util.NewQueryParser(c)
	match := parser.Match([]string{"kind"})

	if con := parser.IContains([]string{"name", "username"}); con != nil {
		if match != nil {
			for i, v := range con {
				match[i] = v
			}
		} else {
			match = con
		}
	}

	query := dbc.Find(match)

	if order := parser.OrderBy(); order != "" {
		query.Sort(order)
	}

	var credentials []models.Credential
	// new mongodb iterator
	iter := query.Iter()
	// loop through each result and modify for our needs
	var tmpCred models.Credential
	// iterate over all and only get valid objects
	for iter.Next(&tmpCred) {
		// if the user doesn't have access to credential
		// skip to next
		if !roles.CredentialRead(user, tmpCred) {
			continue
		}
		// hide passwords, keys even they are already encrypted
		hideEncrypted(&tmpCred)
		if err := metadata.CredentialMetadata(&tmpCred); err != nil {
			log.Println("Error while setting metatdata:", err)
			c.JSON(http.StatusInternalServerError, models.Error{
				Code:http.StatusInternalServerError,
				Message: "Error while getting Credentials",
			})
			return
		}
		// good to go add to list
		credentials = append(credentials, tmpCred)
	}
	if err := iter.Close(); err != nil {
		log.Println("Error while retriving Credential data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while getting Credential",
		})
		return
	}

	count := len(credentials)
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
		Results: credentials[pgi.Skip():pgi.End()],
	})
}

func AddCredential(c *gin.Context) {
	user := c.MustGet(_CTX_USER).(models.User)

	var req models.Credential

	if err := c.BindJSON(&req); err != nil {
		log.Println("Bad payload:", err)
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Message: "Bad Request",
		})
		return
	}

	req.ID = bson.NewObjectId()
	req.CreatedByID = user.ID
	req.ModifiedByID = user.ID
	req.Created = time.Now()
	req.Modified = time.Now()

	if req.Password != nil {
		password := crypt.Encrypt(*req.Password)
		req.Password = &password
	}

	if req.Password != nil {
		data := crypt.Encrypt(*req.SshKeyData)
		req.SshKeyData = &data

		if req.SshKeyUnlock != nil {
			unlock := crypt.Encrypt(*req.SshKeyUnlock)
			req.SshKeyUnlock = &unlock
		}
	}

	if req.Password != nil {
		password := crypt.Encrypt(*req.BecomePassword)
		req.BecomePassword = &password
	}
	if req.Password != nil {
		password := crypt.Encrypt(*req.VaultPassword)
		req.VaultPassword = &password
	}

	collection := db.C(db.CREDENTIALS)

	err := collection.Insert(req)
	if err != nil {
		log.Println("Error while creating Credential:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while creating Credential",
		})
		return
	}

	err = roles.AddCredentialUser(req, user.ID, roles.CREDENTIAL_ADMIN)
	if err != nil {
		log.Println("Error while adding the user to roles:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while adding the user to roles",
		})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Credential " + req.Name + " created")
	hideEncrypted(&req)
	if err := metadata.CredentialMetadata(&req); err != nil {
		log.Println("Error while setting metatdata:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while setting metadata",
		})
	}

	// send response with JSON rendered data
	c.JSON(http.StatusCreated, req)
}

func UpdateCredential(c *gin.Context) {

	user := c.MustGet(_CTX_USER).(models.User)
	credential := c.MustGet(_CTX_CREDENTIAL).(models.Credential)

	var req models.Credential
	if err := c.BindJSON(&req); err != nil {
		// Return 400 if request has bad JSON format
		c.JSON(http.StatusBadRequest, models.Error{
			Code:http.StatusBadRequest,
			Message: "Bad Request",
		})
		return
	}

	if req.Password != nil {
		password := crypt.Encrypt(*req.Password)
		credential.Password = &password
	}

	if req.Password != nil {
		data := crypt.Encrypt(*req.SshKeyData)
		credential.SshKeyData = &data

		if req.SshKeyUnlock != nil {
			unlock := crypt.Encrypt(*credential.SshKeyUnlock)
			credential.SshKeyUnlock = &unlock
		}
	}

	if req.Password != nil {
		password := crypt.Encrypt(*req.BecomePassword)
		credential.BecomeUsername = &password
	}
	if req.Password != nil {
		password := crypt.Encrypt(*req.VaultPassword)
		credential.VaultPassword = &password
	}

	// system generated
	req.ID = credential.ID
	req.CreatedByID = credential.CreatedByID
	req.Created = credential.Created
	req.ModifiedByID = user.ID
	req.Modified = time.Now()

	dbc := db.C(db.CREDENTIALS)

	if err := dbc.UpdateId(credential.ID, req); err != nil {
		log.Println("Failed to update Credential", err)

		c.JSON(http.StatusInternalServerError,
			gin.H{"status": "error", "message": "Failed to update Credential"})
		return
	}

	// add new activity to activity stream
	addActivity(req.ID, user.ID, "Credential " + req.Name + " updated")

	hideEncrypted(&req)
	metadata.CredentialMetadata(&req)

	c.JSON(http.StatusNoContent, req)
}

func RemoveCredential(c *gin.Context) {
	crd := c.MustGet(_CTX_CREDENTIAL).(models.Credential)
	u := c.MustGet(_CTX_USER).(models.User)

	dbc := db.C(db.CREDENTIALS)

	if err := dbc.RemoveId(crd.ID); err != nil {
		log.Println("Failed to remove Credential", err)

		c.JSON(http.StatusInternalServerError,
			gin.H{"status": "error", "message": "Failed to remove Credential"})
		return
	}

	// add new activity to activity stream
	addActivity(crd.ID, u.ID, "Credential " + crd.Name + " deleted")

	c.AbortWithStatus(204)
}

func OwnerTeams(c *gin.Context) {
	credential := c.MustGet(_CTX_CREDENTIAL).(models.Credential)

	var tms []models.Team
	collection := db.C(db.TEAMS)

	for _, v := range credential.Roles {
		if v.Type == "team" {
			var team models.Team
			err := collection.FindId(v.TeamID).One(&team)
			if err != nil {
				log.Println("Error while getting owner teams for credential", credential.ID, err)
				continue //skip iteration
			}
			// set additional info and append to slice
			metadata.TeamMetadata(&team)
			tms = append(tms, team)
		}
	}

	count := len(tms)
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
		Results: tms[pgi.Skip():pgi.End()],
	})
}

func OwnerUsers(c *gin.Context) {
	credential := c.MustGet(_CTX_CREDENTIAL).(models.Credential)

	var usrs []models.User
	collection := db.C(db.USERS)

	for _, v := range credential.Roles {
		if v.Type == "user" {
			var user models.User
			err := collection.FindId(v.UserID).One(&user)
			if err != nil {
				log.Println("Error while getting owner users for credential", credential.ID, err)
				continue //skip iteration
			}
			// set additional info and append to slice
			metadata.UserMetadata(&user)
			usrs = append(usrs, user)
		}
	}

	count := len(usrs)
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
		Results: usrs[pgi.Skip():pgi.End()],
	})
}

// TODO: not complete
func ActivityStream(c *gin.Context) {
	credential := c.MustGet(_CTX_CREDENTIAL).(models.Credential)

	var activities []models.Activity
	collection := db.C(db.ACTIVITY_STREAM)
	err := collection.Find(bson.M{"object_id": credential.ID, "type": _CTX_CREDENTIAL}).All(activities)

	if err != nil {
		log.Println("Error while retriving Activity data from the db:", err)
		c.JSON(http.StatusInternalServerError, models.Error{
			Code:http.StatusInternalServerError,
			Message: "Error while Activities",
		})
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