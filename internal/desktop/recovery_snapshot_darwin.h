#include <stdint.h>
void zwaiRecoverySnapshot(void *window, uint64_t request);
void zwaiRecoveryEvaluate(void *window, const char *script, const char *coverScript);
void zwaiRecoveryCover(void *window, unsigned char red, unsigned char green, unsigned char blue);
