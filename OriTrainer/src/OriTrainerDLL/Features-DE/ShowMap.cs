using System;

namespace OriTrainerDLL.FeaturesDE
{
    // 显示地图：打开游戏自带的"未探索地图可见"调试位，整张地图（地形 + 图标 + 迷雾）
    // 一并按"已发现"绘制，观感等同于插满了所有地图石。
    //
    // ---- 为什么必须挂主线程钩子 ----
    // 游戏自己提供了公开方法 AreaMapDebugNavigation.ToggleUndiscoveredMap(bool)，
    // 它不是单纯赋值，尾部还会刷新已经画好的地图：
    //     UndiscoveredMapVisible = value;
    //     m_areaMapUi.ResetMaps();   // → AreaMapCanvas.ResetMap: SetActive /
    //                                //   Material.SetTexture / RenderTexture + GL 重绘
    //     m_areaMapUi.Navigation.UpdateScrollLimits();
    // 这些是 Unity API，必须在主线程执行（参见此前从定时器线程调 Application.Quit()
    // 崩掉游戏的教训）。所以本功能与 InfiniteDash / InfiniteDoubleJump 一样挂到
    //     Game.Events.Scheduler.OnGameFixedUpdate
    // 只写字段不走 Toggle 也是不行的：地图已经开着时不会有任何重绘，必须靠 ResetMaps。
    //
    // 持续写入是必要的：AreaMapUI.OnDestroy 会把 Instance 置 null，场景重建后是全新对象，
    // 该位回到默认 false。钩子每帧重读 Instance，只在值不对时才写（见 OnGameFixedUpdate）。
    //
    // ---- 停止即真还原 ----
    // Stop() 只落 _running 标志：它跑在命令线程上，而还原要调 ToggleUndiscoveredMap(false)，
    // 同样碰 Unity API。真正的还原交给主线程在下一帧完成，并随即注销钩子。
    // 还原后地图恢复成真实探索状态。
    //
    // ---- 已知偏离 ----
    // 该位不只是"显示"：除地图绘制外，它还被两个 Condition 读取：
    //   HasVisitedOrDiscoveredAreaCondition.Validate —— 置位时直接 return Visited，
    //                                                  完全绕过 m_area.AreaDiscovered
    //   WorldMapOverworldAreaCondition.Validate
    // 这两个类在本程序集里没有任何 IL 调用者（只挂在 prefab 数据上），实际影响未观察到；
    // 但它们回答的是"这个区域算不算已发现"，语义上已超出显示范畴。
    // 若遇到与"区域未发现"相关的怪事，优先怀疑此处。
    //
    // 该位是纯运行时显示位，不写进存档（AreaMapDebugNavigation 没有 Serialize），
    // 也不碰 CheatsHandler.DebugEnabled / DebugWasEnabled，因此不影响成就。
    public static class ShowMap
    {
        private static Action _hook;            // 保留引用以便还原后注销
        private static volatile bool _running;  // 由命令线程写、主线程读

        public static void Start()
        {
            // 先置位再判断：Stop 之后钩子还没来得及注销时再次 Start，复用现成钩子即可。
            _running = true;

            if (_hook != null) return; // 幂等：钩子已在，不重复挂载

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            // 游戏尚未启动到 GameController 时取出会得到 null，直接报错更易排查。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            // 只落标志。还原必须走 Unity API（见文件头），只能交给主线程，
            // 由下一次 OnGameFixedUpdate 完成后注销钩子。
            _running = false;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺 GameController.FixedUpdate 冒进 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                bool running = _running;

                AreaMapUI ui = AreaMapUI.Instance;
                AreaMapDebugNavigation nav = ui?.DebugNavigation;

                // nav 为 null 表示地图 UI 还没建好（未进游戏）或已销毁，此时既无处可写
                // 也无处可还原：新场景里的 AreaMapDebugNavigation 是新建的，该位默认 false。
                // 值已经对了就不动，避免每帧重建 RenderTexture。
                if (nav != null && nav.UndiscoveredMapVisible != running)
                    nav.ToggleUndiscoveredMap(running);

                // 停止后：还原已落地（或本就无可还原），注销自己。
                // 在回调内部注销是安全的：UberDelegate.Call 按下标遍历 m_registers，
                // 而 Remove 走的 ListExtension.RemoveUnordered 是"与尾元素交换后 RemoveAt"，
                // 不会让枚举失效；最坏只让排在本回调之后的回调少跑一帧。
                if (!running)
                {
                    Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
                    _hook = null;
                }
            }
            catch { }
        }
    }
}
