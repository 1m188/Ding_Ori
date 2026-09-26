using System;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 获得三把钥匙：把 Keys.GinsoTree / ForlornRuins / MountHoru 三个静态标记保持为 true。
    // 实现同终极版：直接赋值等价于游戏自己捡到钥匙（开门判定读的就是这三个静态 bool）；
    // 必须持续写是因为死亡恢复检查点会把钥匙打回成检查点里的值（SeinWorldState.Instance
    // 在恢复白名单里）；停止不还原（SeinWorldState.Serialize 会持久化这三个字段）。
    public static class GrantKeys
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

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
                if (!Sein.World.Keys.GinsoTree) Sein.World.Keys.GinsoTree = true;         // Ginso Tree 门钥匙
                if (!Sein.World.Keys.ForlornRuins) Sein.World.Keys.ForlornRuins = true;   // Forlorn Ruins 门钥匙
                if (!Sein.World.Keys.MountHoru) Sein.World.Keys.MountHoru = true;         // Mount Horu 门钥匙
            }
            catch { }
        }
    }
}
