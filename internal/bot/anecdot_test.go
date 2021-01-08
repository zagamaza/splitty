package bot

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/bot/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAnecdot_Help(t *testing.T) {
	require.Equal(t, "анекдот!, анкедот!, joke!, chuck! _– расскажет анекдот или шутку_\n", Anecdote{}.Help())
}

func TestAnecdot_ReactsOnJokeRequest(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader("joke")),
	}, nil)

	response := b.OnMessage(Message{Text: "joke!"})
	require.True(t, response.Send)
	require.Equal(t, "joke", response.Text)
}

func TestAnecdot_ReactsOnJokeRequestAlt(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader("joke")),
	}, nil)

	response := b.OnMessage(Message{Text: "joke!"})
	require.True(t, response.Send)
	require.Equal(t, "joke", response.Text)
}

func TestAnecdot_RshunemaguRetursnNothingOnUnableToDoReq(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(nil, errors.New("err"))

	response := b.rzhunemogu()
	require.False(t, response.Send)
	require.Empty(t, response.Text)
}

func TestAnecdotReactsOnUnexpectedMessage(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	result := b.OnMessage(Message{Text: "unexpected msg"})
	require.False(t, result.Send)
	assert.Empty(t, result.Text)
}

func TestAnecdotReactsOnBadChuckMessage(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(&http.Response{
		Body: ioutil.NopCloser(bytes.NewReader([]byte(`not a json`))),
	}, nil)

	require.Equal(t, Response{}, b.OnMessage(Message{Text: "chuck!"}))
}

func TestAnecdotReactsOnChuckMessageUnableToDoReq(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(nil, errors.New("err"))

	require.Equal(t, Response{}, b.OnMessage(Message{Text: "chuck!"}))
}

func TestAnecdotReactsOnChuckMessage(t *testing.T) {
	mockHTTP := &mocks.HTTPClient{}
	b := NewAnecdote(mockHTTP)

	mockHTTP.On("Do", mock.Anything).Return(&http.Response{
		Body: ioutil.NopCloser(bytes.NewReader([]byte(`{"Value" : {"Joke" : "&quot;joke&quot;"}}`))),
	}, nil)

	require.Equal(t, Response{Text: "- \"joke\"", Send: true}, b.OnMessage(Message{Text: "chuck!"}))
}
