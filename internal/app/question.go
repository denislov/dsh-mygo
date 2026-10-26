package app

import (
	"context"
	"encoding/json"
	"errors"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func (a *App) publishQuestions() {
	a.model.Questions = a.questions.Items()
	a.model.QuestionOpen = len(a.model.Questions) > 0
}

func (a *App) clearQuestions() {
	a.questions.Clear()
	a.publishQuestions()
}

func (a *App) handleQuestionNotification(method string, params json.RawMessage) {
	if method == protocol.NotificationQuestionClosed {
		requestID, sessionID, err := interaction.DecodeQuestionClosed(params)
		if err != nil {
			a.setFailure("Invalid question notification", err)
			go a.stopHost()
			return
		}
		a.questions.Remove(requestID, sessionID)
		a.publishQuestions()
		return
	}
	request, err := interaction.DecodeQuestion(params)
	if err != nil {
		a.setFailure("Invalid question request", err)
		go a.stopHost()
		return
	}
	if request.SessionID != "" && request.SessionID != a.sessionID {
		return
	}
	if !a.model.CanQuestion {
		a.setFailure("Unexpected question request", errors.New("Question capability was not negotiated"))
		go a.stopHost()
		return
	}
	if err := a.questions.Add(request); err != nil {
		a.setFailure("Invalid question queue", err)
		go a.stopHost()
		return
	}
	a.publishQuestions()
}

func (a *App) RespondQuestion(requestID string, cancelQuestion bool) {
	if a.shuttingDown.Load() || a.options.SDKProtocol || !a.model.CanQuestion || a.model.CancelPending || a.model.Phase != view.PhaseRunning && a.model.Phase != view.PhaseIdle || len(a.model.Approvals) > 0 {
		return
	}
	items := a.questions.Items()
	if len(items) == 0 || items[0].RequestID != requestID || items[0].Responding {
		return
	}
	client := a.currentClient()
	if client == nil {
		return
	}
	method := protocol.MethodQuestionCancel
	var params any = protocol.QuestionCancelParams{RequestID: requestID}
	if !cancelQuestion {
		answers, err := items[0].Answers()
		if err != nil {
			a.questions.Fail(requestID, err.Error())
			a.publishQuestions()
			return
		}
		wireAnswers := make([]protocol.QuestionAnswer, 0, len(answers))
		for _, answer := range answers {
			wireAnswers = append(wireAnswers, protocol.QuestionAnswer{ID: answer.ID, Selected: answer.Selected, Custom: answer.Custom})
		}
		method = protocol.MethodQuestionRespond
		params = protocol.QuestionRespondParams{RequestID: requestID, Answers: wireAnswers}
	}
	sessionID := items[0].SessionID
	if !a.questions.Begin(requestID) {
		return
	}
	a.publishQuestions()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
		defer cancel()
		result, err := client.Call(ctx, method, params)

		a.post(func() {
			if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
				return
			}
			var rpcError *channel.RPCError
			stale := errors.As(err, &rpcError) && protocol.ErrorKind(rpcError.Data) == protocol.ErrorKindQuestionStaleRequest
			var acknowledged map[string]json.RawMessage
			if err == nil && (json.Unmarshal(result, &acknowledged) != nil || acknowledged == nil) {
				err = errors.New("invalid question response")
			}
			if err == nil || stale {
				a.questions.Remove(requestID, sessionID)
			} else {
				a.questions.Fail(requestID, "Could not send the answer. Retry or cancel the question.")
			}
			a.publishQuestions()
		})
	}()
}
