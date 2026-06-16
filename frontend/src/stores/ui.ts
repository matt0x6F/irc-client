import { create } from 'zustand';

// Settings is now its own native window (opened from the Go backend via
// App.OpenSettings), not an in-app modal — so no settings state lives here.

interface UIState {
  // Topic modal
  showTopicModal: boolean;
  setShowTopicModal: (show: boolean) => void;

  // Mode modal
  showModeModal: boolean;
  setShowModeModal: (show: boolean) => void;

  // User info
  showUserInfo: { networkId: number; nickname: string } | null;
  setShowUserInfo: (info: { networkId: number; nickname: string } | null) => void;

  // Search modal
  showSearch: boolean;
  toggleSearch: () => void;
  openSearch: () => void;
  closeSearch: () => void;

  // Channel list modal
  showChannelList: { networkId: number } | null;
  openChannelList: (networkId: number) => void;
  closeChannelList: () => void;

  // Keyboard shortcuts modal
  showKeyboardShortcuts: boolean;
  toggleKeyboardShortcuts: () => void;
  closeKeyboardShortcuts: () => void;

  // Sidebar widths
  leftSidebarWidth: number;
  rightSidebarWidth: number;
  setLeftSidebarWidth: (width: number) => void;
  setRightSidebarWidth: (width: number) => void;

  // Sidebar collapsed state
  leftSidebarCollapsed: boolean;
  rightSidebarCollapsed: boolean;
  toggleLeftSidebar: () => void;
  toggleRightSidebar: () => void;
  setLeftSidebarCollapsed: (collapsed: boolean) => void;
  setRightSidebarCollapsed: (collapsed: boolean) => void;

  // Right sidebar tab (users vs pinned messages)
  rightSidebarTab: 'users' | 'pinned';
  setRightSidebarTab: (tab: 'users' | 'pinned') => void;
}

export const useUIStore = create<UIState>((set) => ({
  showTopicModal: false,
  setShowTopicModal: (show) => set({ showTopicModal: show }),

  showModeModal: false,
  setShowModeModal: (show) => set({ showModeModal: show }),

  showUserInfo: null,
  setShowUserInfo: (info) => set({ showUserInfo: info }),

  showSearch: false,
  toggleSearch: () => set((state) => ({ showSearch: !state.showSearch })),
  openSearch: () => set({ showSearch: true }),
  closeSearch: () => set({ showSearch: false }),

  showChannelList: null,
  openChannelList: (networkId) => set({ showChannelList: { networkId } }),
  closeChannelList: () => set({ showChannelList: null }),

  showKeyboardShortcuts: false,
  toggleKeyboardShortcuts: () => set((state) => ({ showKeyboardShortcuts: !state.showKeyboardShortcuts })),
  closeKeyboardShortcuts: () => set({ showKeyboardShortcuts: false }),

  leftSidebarWidth: 256,
  rightSidebarWidth: 256,
  setLeftSidebarWidth: (width) => set({ leftSidebarWidth: width }),
  setRightSidebarWidth: (width) => set({ rightSidebarWidth: width }),

  leftSidebarCollapsed: false,
  rightSidebarCollapsed: false,
  toggleLeftSidebar: () => set((state) => ({ leftSidebarCollapsed: !state.leftSidebarCollapsed })),
  toggleRightSidebar: () => set((state) => ({ rightSidebarCollapsed: !state.rightSidebarCollapsed })),
  setLeftSidebarCollapsed: (collapsed) => set({ leftSidebarCollapsed: collapsed }),
  setRightSidebarCollapsed: (collapsed) => set({ rightSidebarCollapsed: collapsed }),

  rightSidebarTab: 'users',
  setRightSidebarTab: (tab) => set({ rightSidebarTab: tab }),
}));
