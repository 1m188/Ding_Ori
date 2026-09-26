using System;
using System.Collections.Generic;
using System.Reflection;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 100% 探索：把所有区域的完成度缓存直接写成 1.0（即界面 100%）。
    // 实现同终极版：m_completionAmount 是缓存、m_dirtyCompletionAmount 置真时会被
    // UpdateCompletionAmount() 重算覆盖，所以每周期两件事都做（写 1.0 + 清 dirty）。
    // 目标是用 AchievementsLogic 每 5 秒采样读到 1.0 拿成就；停止不还原
    // （纯运行时缓存，不进存档，成就一经授予即永久）。
    public static class CompleteExploration
    {
        private const int IntervalMs = 10;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fAmount; // m_completionAmount (Single, 0..1)
        private static FieldInfo _fDirty;  // m_dirtyCompletionAmount (Boolean)
        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            Type t = typeof(RuntimeGameWorldArea);
            _fAmount = t.GetField("m_completionAmount", Private);
            _fDirty = t.GetField("m_dirtyCompletionAmount", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每 10ms 静默空转
            if (_fAmount == null || _fDirty == null)
                throw new Exception("RuntimeGameWorldArea 的字段名与预期不符，功能无法工作");

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
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
