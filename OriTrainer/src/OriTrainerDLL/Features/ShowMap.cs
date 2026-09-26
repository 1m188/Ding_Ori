using System;

namespace OriTrainerDLL.Features
{
    // 显示地图：打开游戏自带的"未探索地图可见"调试位，整张地图按"已发现"绘制。
    // 实现同终极版，必须挂主线程钩子：ToggleUndiscoveredMap 尾部会刷新已画好的地图
    // （ResetMaps → RenderTexture + GL 重绘，Unity API），所以挂
    // Game.Events.Scheduler.OnGameFixedUpdate。停止即真还原（还原也走 Unity API，
    // 由主线程下一帧完成）。纯运行时显示位，不写存档、不影响成就。
    public static class ShowMap
    {
        private static Action _hook;            // 保留引用以便还原后注销
        private static volatile bool _running;  // 由命令线程写、主线程读

        public static void Start()
        {
            // 先置位再判断：Stop 之后钩子还没来得及注销时再次 Start，复用现成钩子即可。
            _running = true;

            if (_hook != null) return; // 幂等：钩子已在，不重复挂载

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
                // 也无处可还原。值已经对了就不动，避免每帧重建 RenderTexture。
                if (nav != null && nav.UndiscoveredMapVisible != running)
                    nav.ToggleUndiscoveredMap(running);

                // 停止后：还原已落地（或本就无可还原），注销自己。
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
