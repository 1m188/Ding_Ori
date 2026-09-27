using System;
using System.Collections.Generic;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 100% 探索：把所有区域的完成度缓存直接写成 1.0（即界面 100%）。
    // 实现同终极版：m_completionAmount 是缓存、m_dirtyCompletionAmount 置真时会被
    // UpdateCompletionAmount() 重算覆盖，所以每周期两件事都做（写 1.0 + 清 dirty）。
    // 目标是用 AchievementsLogic 每 5 秒采样读到 1.0 拿成就；停止不还原
    // （纯运行时缓存，不进存档，成就一经授予即永久）。
    //
    // 挂游戏自己的每帧回调 OnGameFixedUpdate（原版旧 Mono 的 System.Threading.Timer
    // 不可靠，回调不触发）。
    public static class CompleteExploration
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fAmount; // m_completionAmount (Single, 0..1)
        private static FieldInfo _fDirty;  // m_dirtyCompletionAmount (Boolean)
        private static Action _hook;       // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            Type t = typeof(RuntimeGameWorldArea);
            _fAmount = t.GetField("m_completionAmount", Private);
            _fDirty = t.GetField("m_dirtyCompletionAmount", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            if (_fAmount == null || _fDirty == null)
                throw new Exception("RuntimeGameWorldArea 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                GameWorld world = GameWorld.Instance;
                if (world == null) return;

                List<RuntimeGameWorldArea> areas = world.RuntimeAreas;
                if (areas == null) return;

                for (int i = 0; i < areas.Count; i++)
                {
                    RuntimeGameWorldArea area = areas[i];
                    if (area == null) continue;

                    _fAmount.SetValue(area, 1f);
                    _fDirty.SetValue(area, false); // 不清就会被 UpdateCompletionAmount 覆盖
                }
            }
            catch { }
        }
    }
}