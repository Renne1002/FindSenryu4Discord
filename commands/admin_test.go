package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCanManageChannel(t *testing.T) {
	tests := []struct {
		name        string
		interaction *discordgo.InteractionCreate
		want        bool
	}{
		{
			name: "Administratorのみ",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionAdministrator,
					},
				},
			},
			want: true,
		},
		{
			name: "ManageChannelsのみ",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionManageChannels,
					},
				},
			},
			want: true,
		},
		{
			name: "両方の権限あり",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionAdministrator | discordgo.PermissionManageChannels,
					},
				},
			},
			want: true,
		},
		{
			name: "どちらの権限もなし",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionSendMessages,
					},
				},
			},
			want: false,
		},
		{
			name: "権限ゼロ",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: 0,
					},
				},
			},
			want: false,
		},
		{
			name: "MemberがnilのDM",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: nil,
				},
			},
			want: false,
		},
		{
			name: "ManageChannels含む複数権限",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionManageChannels | discordgo.PermissionSendMessages | discordgo.PermissionViewChannel,
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canManageChannel(tt.interaction)
			if got != tt.want {
				t.Errorf("canManageChannel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsServerAdmin(t *testing.T) {
	tests := []struct {
		name        string
		interaction *discordgo.InteractionCreate
		want        bool
	}{
		{
			name: "Administratorあり",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionAdministrator,
					},
				},
			},
			want: true,
		},
		{
			name: "ManageChannelsのみはfalse",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: &discordgo.Member{
						Permissions: discordgo.PermissionManageChannels,
					},
				},
			},
			want: false,
		},
		{
			name: "MemberがnilのDM",
			interaction: &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Member: nil,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isServerAdmin(tt.interaction)
			if got != tt.want {
				t.Errorf("isServerAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}
