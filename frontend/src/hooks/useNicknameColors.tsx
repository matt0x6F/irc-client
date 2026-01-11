import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { GetNicknameColorsBatch } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

interface NetworkColorCache {
  colors: { [nickname: string]: string };
  lastUpdate: number;
}

interface ColorCache {
  [networkId: number]: NetworkColorCache;
}

export function useNicknameColors(networkId: number | null, nicknames: string[]) {
  const [colors, setColors] = useState<Map<string, string>>(new Map());
  const cacheRef = useRef<ColorCache>({});
  const CACHE_TTL = 60000; // 1 minute

  // Memoize nicknames array to avoid unnecessary re-renders
  const nicknamesKey = useMemo(() => {
    return [...nicknames].sort().join(',');
  }, [nicknames]);

  // Fetch colors for nicknames
  const fetchColors = useCallback(async () => {
    if (!networkId || nicknames.length === 0) {
      setColors(new Map());
      return;
    }
    
    // Check cache first - but only use cached colors if ALL requested nicknames are cached
    const cache = cacheRef.current[networkId];
    const now = Date.now();
    const cached = cache && (now - cache.lastUpdate) < CACHE_TTL;
    
    if (cached && cache.colors) {
      const cachedColors = new Map<string, string>();
      let allCached = true;
      nicknames.forEach(nick => {
        const color = cache.colors[nick];
        if (color) {
          cachedColors.set(nick, color);
        } else {
          allCached = false;
        }
      });
      // Only use cache if we have colors for ALL requested nicknames
      if (allCached && cachedColors.size === nicknames.length) {
        setColors(cachedColors);
        return;
      }
      // If we have some cached colors, use them but still fetch the rest
      if (cachedColors.size > 0) {
        setColors(cachedColors);
      }
    }
    
    // Fetch from backend (always fetch to ensure we have latest colors)
    try {
      const colorMap = await GetNicknameColorsBatch(networkId, nicknames);
      console.log('[useNicknameColors] Fetched colors:', colorMap, 'for networkId:', networkId, 'nicknames:', nicknames);
      console.log('[useNicknameColors] Color map entries:', Object.keys(colorMap).length, 'colors found');
      
      // Update cache
      if (!cacheRef.current[networkId]) {
        cacheRef.current[networkId] = { colors: {}, lastUpdate: now };
      }
      Object.entries(colorMap).forEach(([nick, color]) => {
        if (typeof color === 'string') {
          cacheRef.current[networkId].colors[nick] = color;
          console.log('[useNicknameColors] Cached color for', nick, ':', color);
        }
      });
      cacheRef.current[networkId].lastUpdate = now;
      
      // Merge with existing colors (in case we had some cached)
      setColors(prev => {
        const next = new Map(prev);
        Object.entries(colorMap).forEach(([nick, color]) => {
          if (typeof color === 'string') {
            next.set(nick, color);
          }
        });
        console.log('[useNicknameColors] Setting colors state with', next.size, 'entries');
        return next;
      });
    } catch (error) {
      console.error('[useNicknameColors] Failed to fetch nickname colors:', error);
    }
  }, [networkId, nicknamesKey]);

  // Initial fetch when nicknames change (for loading existing messages)
  useEffect(() => {
    fetchColors();
  }, [fetchColors]);

  // Listen for metadata updates (when plugin generates colors) - this is the primary event-driven mechanism
  useEffect(() => {
    if (!networkId) return;
    
    const unsubscribe = EventsOn('metadata-updated', (data: any) => {
      // Handle network_id comparison (can be number, null, or undefined)
      const eventNetworkId = data.network_id != null ? Number(data.network_id) : null;
      if (eventNetworkId === networkId && data.type === 'nickname_color') {
        const nick = data.key.replace('nickname:', '');
        setColors(prev => {
          const next = new Map(prev);
          if (data.value) {
            next.set(nick, data.value);
            // Update cache
            if (!cacheRef.current[networkId]) {
              cacheRef.current[networkId] = { colors: {}, lastUpdate: Date.now() };
            }
            if (typeof data.value === 'string') {
              cacheRef.current[networkId].colors[nick] = data.value;
            }
          } else {
            next.delete(nick);
            if (cacheRef.current[networkId]?.colors) {
              delete cacheRef.current[networkId].colors[nick];
            }
          }
          return next;
        });
      }
    });
    
    return unsubscribe;
  }, [networkId]);

  // Listen for user join events to fetch colors for newly joined users
  // This ensures we get colors immediately when users join, even if metadata-updated hasn't fired yet
  useEffect(() => {
    if (!networkId) return;
    
    const unsubscribe = EventsOn('message-event', (data: any) => {
      // Check if this is a user.joined event for our network
      if (data.type === 'user.joined') {
        const eventData = data.data || data;
        const eventNetworkId = eventData.networkId != null ? Number(eventData.networkId) : null;
        if (eventNetworkId === networkId && eventData.user) {
          const user = eventData.user;
          // If this user is in our nicknames list but we don't have their color yet, fetch it
          if (nicknames.includes(user) && !colors.has(user)) {
            // Fetch colors for this specific user
            GetNicknameColorsBatch(networkId, [user]).then(colorMap => {
              if (colorMap[user]) {
                setColors(prev => {
                  const next = new Map(prev);
                  next.set(user, colorMap[user]);
                  // Update cache
                  if (!cacheRef.current[networkId]) {
                    cacheRef.current[networkId] = { colors: {}, lastUpdate: Date.now() };
                  }
                  cacheRef.current[networkId].colors[user] = colorMap[user];
                  cacheRef.current[networkId].lastUpdate = Date.now();
                  return next;
                });
              }
            }).catch(err => {
              console.error('[useNicknameColors] Failed to fetch color for joined user:', err);
            });
          }
        }
      }
    });
    
    return unsubscribe;
  }, [networkId, nicknamesKey, colors]);

  return colors;
}
