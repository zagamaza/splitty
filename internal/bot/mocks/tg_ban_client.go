// Code generated by mockery v1.0.0. DO NOT EDIT.

package mocks

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	mock "github.com/stretchr/testify/mock"
)

// TgBanClient is an autogenerated mock type for the TgBanClient type
type TgBanClient struct {
	mock.Mock
}

// KickChatMember provides a mock function with given fields: config
func (_m *TgBanClient) KickChatMember(config tgbotapi.KickChatMemberConfig) (tgbotapi.APIResponse, error) {
	ret := _m.Called(config)

	var r0 tgbotapi.APIResponse
	if rf, ok := ret.Get(0).(func(tgbotapi.KickChatMemberConfig) tgbotapi.APIResponse); ok {
		r0 = rf(config)
	} else {
		r0 = ret.Get(0).(tgbotapi.APIResponse)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(tgbotapi.KickChatMemberConfig) error); ok {
		r1 = rf(config)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// UnbanChatMember provides a mock function with given fields: config
func (_m *TgBanClient) UnbanChatMember(config tgbotapi.ChatMemberConfig) (tgbotapi.APIResponse, error) {
	ret := _m.Called(config)

	var r0 tgbotapi.APIResponse
	if rf, ok := ret.Get(0).(func(tgbotapi.ChatMemberConfig) tgbotapi.APIResponse); ok {
		r0 = rf(config)
	} else {
		r0 = ret.Get(0).(tgbotapi.APIResponse)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(tgbotapi.ChatMemberConfig) error); ok {
		r1 = rf(config)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
