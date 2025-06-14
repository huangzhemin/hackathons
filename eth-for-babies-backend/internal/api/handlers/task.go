package handlers

import (
	"net/http"
	"strconv"
	"time"

	"eth-for-babies-backend/internal/models"
	"eth-for-babies-backend/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	db *gorm.DB
}

func NewTaskHandler(db *gorm.DB) *TaskHandler {
	return &TaskHandler{db: db}
}

type CreateTaskRequest struct {
	Title            string `json:"title" binding:"required"`
	Description      string `json:"description" binding:"required"`
	RewardAmount     string `json:"reward_amount" binding:"required"`
	Difficulty       string `json:"difficulty" binding:"required"`
	AssignedChildID  *uint  `json:"assigned_child_id,omitempty"`
	DueDate          string `json:"due_date,omitempty"`
}

type UpdateTaskRequest struct {
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	RewardAmount    string `json:"reward_amount,omitempty"`
	Difficulty      string `json:"difficulty,omitempty"`
	Status          string `json:"status,omitempty"`
	AssignedChildID *uint  `json:"assigned_child_id,omitempty"`
	DueDate         string `json:"due_date,omitempty"`
}

type CompleteTaskRequest struct {
	CompletionProof string `json:"completion_proof" binding:"required"`
}

type RejectTaskRequest struct {
	Reason string `json:"reason,omitempty"`
}

// CreateTask 创建任务
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data",
		})
		return
	}

	// 获取当前用户信息
	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	role, exists := c.Get("role")
	if !exists || role != "parent" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only parents can create tasks",
		})
		return
	}

	// 验证难度
	if !utils.IsValidDifficulty(req.Difficulty) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid difficulty. Must be 'easy', 'medium', or 'hard'",
		})
		return
	}

	// 如果指定了孩子，验证孩子是否属于当前父母
	if req.AssignedChildID != nil {
		var child models.Child
		result := h.db.Where("id = ? AND parent_address = ?", *req.AssignedChildID, walletAddress).First(&child)
		if result.Error == gorm.ErrRecordNotFound {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Child not found or not belongs to you",
			})
			return
		} else if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Database error",
			})
			return
		}
	}

	// 创建任务
	task := models.Task{
		Title:           utils.SanitizeString(req.Title),
		Description:     utils.SanitizeString(req.Description),
		RewardAmount:    req.RewardAmount,
		Difficulty:      req.Difficulty,
		Status:          "pending",
		CreatedBy:       walletAddress.(string),
		AssignedChildID: req.AssignedChildID,
	}

	// 解析截止日期
	if req.DueDate != "" {
		dueDate, err := time.Parse(time.RFC3339, req.DueDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid due date format. Use RFC3339 format",
			})
			return
		}
		task.DueDate = &dueDate
	}

	// 如果分配给了孩子，状态改为进行中
	if req.AssignedChildID != nil {
		task.Status = "in_progress"
	}

	if err := h.db.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to create task",
		})
		return
	}

	// 预加载关联数据
	h.db.Preload("AssignedChild").First(&task, task.ID)

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    task,
	})
}

// GetTasks 获取任务列表
func (h *TaskHandler) GetTasks(c *gin.Context) {
	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	role, exists := c.Get("role")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User role not found",
		})
		return
	}

	// 获取查询参数
	childIDParam := c.Query("child_id")
	status := c.Query("status")

	var tasks []models.Task
	query := h.db.Model(&models.Task{})

	if role == "parent" {
		// 父母可以看到自己创建的所有任务
		query = query.Where("created_by = ?", walletAddress)
		
		// 如果指定了孩子ID，过滤该孩子的任务
		if childIDParam != "" {
			childID, err := strconv.ParseUint(childIDParam, 10, 32)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "Invalid child ID",
				})
				return
			}
			query = query.Where("assigned_child_id = ?", uint(childID))
		}
	} else {
		// 孩子只能看到分配给自己的任务
		var child models.Child
		result := h.db.Where("wallet_address = ?", walletAddress).First(&child)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Child record not found",
			})
			return
		}
		query = query.Where("assigned_child_id = ?", child.ID)
	}

	// 按状态过滤
	if status != "" && utils.IsValidTaskStatus(status) {
		query = query.Where("status = ?", status)
	}

	// 执行查询
	if err := query.Preload("AssignedChild").Order("created_at DESC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to fetch tasks",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tasks,
	})
}

// GetTaskByID 获取任务详情
func (h *TaskHandler) GetTaskByID(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	var task models.Task
	result := h.db.Preload("AssignedChild").First(&task, uint(id))
	if result.Error == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	// 检查权限
	role, _ := c.Get("role")
	if role == "parent" && task.CreatedBy != walletAddress.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Access denied",
		})
		return
	} else if role == "child" {
		// 检查任务是否分配给了这个孩子
		var child models.Child
		result := h.db.Where("wallet_address = ?", walletAddress).First(&child)
		if result.Error != nil || task.AssignedChildID == nil || *task.AssignedChildID != child.ID {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "Access denied",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// UpdateTask 更新任务
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	var req UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data",
		})
		return
	}

	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	// 查找任务
	var task models.Task
	result := h.db.First(&task, uint(id))
	if result.Error == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	// 检查权限（只有任务创建者可以更新）
	if task.CreatedBy != walletAddress.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only task creator can update the task",
		})
		return
	}

	// 更新任务信息
	if req.Title != "" {
		task.Title = utils.SanitizeString(req.Title)
	}
	if req.Description != "" {
		task.Description = utils.SanitizeString(req.Description)
	}
	if req.RewardAmount != "" {
		task.RewardAmount = req.RewardAmount
	}
	if req.Difficulty != "" && utils.IsValidDifficulty(req.Difficulty) {
		task.Difficulty = req.Difficulty
	}
	if req.Status != "" && utils.IsValidTaskStatus(req.Status) {
		task.Status = req.Status
	}
	if req.AssignedChildID != nil {
		// 验证孩子是否属于当前父母
		var child models.Child
		result := h.db.Where("id = ? AND parent_address = ?", *req.AssignedChildID, walletAddress).First(&child)
		if result.Error == gorm.ErrRecordNotFound {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Child not found or not belongs to you",
			})
			return
		} else if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Database error",
			})
			return
		}
		task.AssignedChildID = req.AssignedChildID
		// 如果分配给了孩子且状态是pending，改为in_progress
		if task.Status == "pending" {
			task.Status = "in_progress"
		}
	}
	if req.DueDate != "" {
		dueDate, err := time.Parse(time.RFC3339, req.DueDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Invalid due date format. Use RFC3339 format",
			})
			return
		}
		task.DueDate = &dueDate
	}

	if err := h.db.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to update task",
		})
		return
	}

	// 预加载关联数据
	h.db.Preload("AssignedChild").First(&task, task.ID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// CompleteTask 完成任务
func (h *TaskHandler) CompleteTask(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	var req CompleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data",
		})
		return
	}

	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	role, exists := c.Get("role")
	if !exists || role != "child" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only children can complete tasks",
		})
		return
	}

	// 查找任务
	var task models.Task
	result := h.db.First(&task, uint(id))
	if result.Error == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	// 检查任务是否分配给了当前孩子
	var child models.Child
	result = h.db.Where("wallet_address = ?", walletAddress).First(&child)
	if result.Error != nil || task.AssignedChildID == nil || *task.AssignedChildID != child.ID {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Task not assigned to you",
		})
		return
	}

	// 检查任务状态
	if task.Status != "in_progress" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Task is not in progress",
		})
		return
	}

	// 更新任务状态
	now := time.Now()
	task.Status = "completed"
	task.CompletionProof = &req.CompletionProof
	task.SubmittedAt = &now

	if err := h.db.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to complete task",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// ApproveTask 批准任务
func (h *TaskHandler) ApproveTask(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	role, exists := c.Get("role")
	if !exists || role != "parent" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only parents can approve tasks",
		})
		return
	}

	// 查找任务
	var task models.Task
	result := h.db.Preload("AssignedChild").First(&task, uint(id))
	if result.Error == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	// 检查权限
	if task.CreatedBy != walletAddress.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only task creator can approve the task",
		})
		return
	}

	// 检查任务状态
	if task.Status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Task is not completed yet",
		})
		return
	}

	// 开始事务
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 更新任务状态
	now := time.Now()
	task.Status = "approved"
	task.ApprovedAt = &now

	if err := tx.Save(&task).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to approve task",
		})
		return
	}

	// 更新孩子的统计信息
	if task.AssignedChild != nil {
		if err := tx.Model(&models.Child{}).Where("id = ?", task.AssignedChild.ID).Updates(map[string]interface{}{
			"total_tasks_completed": gorm.Expr("total_tasks_completed + ?", 1),
			"total_rewards_earned":  gorm.Expr("CAST(total_rewards_earned AS REAL) + CAST(? AS REAL)", task.RewardAmount),
		}).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Failed to update child statistics",
			})
			return
		}
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to commit transaction",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// RejectTask 拒绝任务
func (h *TaskHandler) RejectTask(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	var req RejectTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request data",
		})
		return
	}

	walletAddress, exists := c.Get("wallet_address")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "User not authenticated",
		})
		return
	}

	role, exists := c.Get("role")
	if !exists || role != "parent" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only parents can reject tasks",
		})
		return
	}

	// 查找任务
	var task models.Task
	result := h.db.First(&task, uint(id))
	if result.Error == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Database error",
		})
		return
	}

	// 检查权限
	if task.CreatedBy != walletAddress.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "Only task creator can reject the task",
		})
		return
	}

	// 检查任务状态
	if task.Status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Task is not completed yet",
		})
		return
	}

	// 更新任务状态
	now := time.Now()
	task.Status = "rejected"
	task.RejectedAt = &now
	if req.Reason != "" {
		task.RejectionReason = &req.Reason
	}

	if err := h.db.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to reject task",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}