using System;

namespace OriTrainerDLL.Features
{
    // 无限能力点数：一次性把能力点写满，不做持续写入。
    // 实现同终极版：买技能即落盘（SeinLevel.Serialize 把 SkillPoints 写进存档），
    // 停止无意义，故 Stop() 空实现；Current <= 0 时修正为 1 以满足技能树开启条件。
    public static class InfiniteSkillPoints
    {
        private const int TargetSkillPoints = 99;

        public static void Start()
        {
            SeinCharacter sein = Game.Characters.Sein ?? throw new Exception("当前不在游戏中（Sein 为空），无法写入能力点");

            SeinLevel level = sein.Level ?? throw new Exception("SeinLevel 为空，无法写入能力点");

            // 技能树开启条件含 Current > 0，只在尚未升级时修正
            if (level.Current <= 0)
                level.Current = 1;

            level.SkillPoints = TargetSkillPoints;
        }

        // 点数是已经写进存档的状态，停止既不能也不应该收回（见文件头说明）
        public static void Stop()
        {
        }
    }
}
