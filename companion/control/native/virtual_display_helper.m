#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <IOSurface/IOSurface.h>
#include <dispatch/dispatch.h>
#include <mach/mach_time.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

// CGVirtualDisplay is present in macOS but is not in the public Swift module.
// These declarations mirror the runtime class used by DeskPad and other
// virtual-display clients. The app links only against public frameworks; the
// classes are resolved by Objective-C runtime lookup at launch.
@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(NSUInteger)width height:(NSUInteger)height refreshRate:(CGFloat)refreshRate;
@end

@interface CGVirtualDisplaySettings : NSObject
@property(nonatomic) NSArray *modes;
@property(nonatomic) unsigned int hiDPI;
@end

@interface CGVirtualDisplayDescriptor : NSObject
@property(nonatomic) NSString *name;
@property(nonatomic) unsigned int maxPixelsWide;
@property(nonatomic) unsigned int maxPixelsHigh;
@property(nonatomic) CGSize sizeInMillimeters;
@property(nonatomic) unsigned int serialNum;
@property(nonatomic) unsigned int productID;
@property(nonatomic) unsigned int vendorID;
- (void)setDispatchQueue:(dispatch_queue_t)queue;
@end

@interface CGVirtualDisplay : NSObject
@property(readonly, nonatomic) CGDirectDisplayID displayID;
- (instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)descriptor;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)settings;
@end

static volatile sig_atomic_t g_stop = 0;
static int g_client = -1;
static dispatch_queue_t g_stream_queue;

static void on_signal(int signal_number) {
    (void)signal_number;
    g_stop = 1;
}

static int send_all(int fd, const void *buffer, size_t length) {
    const uint8_t *bytes = (const uint8_t *)buffer;
    while (length > 0) {
        ssize_t written = send(fd, bytes, length, 0);
        if (written > 0) {
            bytes += written;
            length -= (size_t)written;
            continue;
        }
        if (written < 0 && (errno == EINTR || errno == EAGAIN)) continue;
        return -1;
    }
    return 0;
}

static void stream_frame(CGDisplayStreamFrameStatus status, uint64_t display_time,
                         IOSurfaceRef surface, CGDisplayStreamUpdateRef update) {
    (void)display_time;
    (void)update;
    if (status != kCGDisplayStreamFrameStatusFrameComplete || surface == NULL || g_client < 0) return;
    const size_t width = 160;
    const size_t height = 128;
    const size_t bytes_per_pixel = 4;
    const size_t payload_length = width * height * bytes_per_pixel;
    uint8_t header[20] = {'V', 'D', 'F', '1', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    uint64_t sequence = (uint64_t)mach_absolute_time();
    memcpy(header + 4, &width, sizeof(uint16_t));
    memcpy(header + 6, &height, sizeof(uint16_t));
    uint32_t payload = (uint32_t)payload_length;
    memcpy(header + 8, &payload, sizeof(uint32_t));
    memcpy(header + 12, &sequence, sizeof(uint64_t));

    if (IOSurfaceLock(surface, kIOSurfaceLockReadOnly, NULL) != kIOReturnSuccess) return;
    const uint8_t *base = (const uint8_t *)IOSurfaceGetBaseAddress(surface);
    size_t row_bytes = IOSurfaceGetBytesPerRow(surface);
    int ok = base != NULL && send_all(g_client, header, sizeof(header)) == 0;
    uint8_t row[160 * 4];
    if (ok) {
        for (size_t y = 0; y < height; y++) {
            memcpy(row, base + y * row_bytes, sizeof(row));
            if (send_all(g_client, row, sizeof(row)) != 0) {
                ok = 0;
                break;
            }
        }
    }
    IOSurfaceUnlock(surface, kIOSurfaceLockReadOnly, NULL);
    if (!ok) {
        close(g_client);
        g_client = -1;
    }
}

static int make_server(const char *path) {
    int server = socket(AF_UNIX, SOCK_STREAM, 0);
    if (server < 0) return -1;
    unlink(path);
    struct sockaddr_un address;
    memset(&address, 0, sizeof(address));
    address.sun_family = AF_UNIX;
    if (strlen(path) >= sizeof(address.sun_path)) {
        close(server);
        return -1;
    }
    strncpy(address.sun_path, path, sizeof(address.sun_path) - 1);
    if (bind(server, (struct sockaddr *)&address, sizeof(address)) != 0 || listen(server, 1) != 0) {
        close(server);
        unlink(path);
        return -1;
    }
    return server;
}

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc != 3 || strcmp(argv[1], "--socket") != 0) {
            fprintf(stderr, "usage: virtual-display-helper --socket PATH\n");
            return 2;
        }
        const char *socket_path = argv[2];
        signal(SIGTERM, on_signal);
        signal(SIGINT, on_signal);
        signal(SIGPIPE, SIG_IGN);

        int server = make_server(socket_path);
        if (server < 0) {
            perror("virtual display socket");
            return 1;
        }

        CGVirtualDisplayDescriptor *descriptor = [CGVirtualDisplayDescriptor new];
        [descriptor setDispatchQueue:dispatch_get_main_queue()];
        descriptor.name = @"MINI TV 虚拟屏幕";
        descriptor.maxPixelsWide = 640;
        descriptor.maxPixelsHigh = 512;
        descriptor.sizeInMillimeters = CGSizeMake(160, 128);
        descriptor.productID = 0x4D54;
        descriptor.vendorID = 0x4D54;
        descriptor.serialNum = (unsigned int)getpid();
        CGVirtualDisplay *display = [[CGVirtualDisplay alloc] initWithDescriptor:descriptor];
        if (display == nil) {
            fprintf(stderr, "CGVirtualDisplay is unavailable on this macOS version\n");
            close(server);
            unlink(socket_path);
            return 1;
        }
        CGVirtualDisplaySettings *settings = [CGVirtualDisplaySettings new];
        settings.hiDPI = 1;
        settings.modes = @[[[CGVirtualDisplayMode alloc] initWithWidth:640 height:512 refreshRate:30.0]];
        if (![display applySettings:settings]) {
            fprintf(stderr, "failed to apply virtual display settings\n");
            close(server);
            unlink(socket_path);
            return 1;
        }
        fprintf(stdout, "READY %u\n", display.displayID);
        fflush(stdout);

        g_stream_queue = dispatch_queue_create("com.muzilee.minitv.virtual-display", DISPATCH_QUEUE_SERIAL);
        while (!g_stop) {
            g_client = accept(server, NULL, NULL);
            if (g_client < 0 && errno == EINTR) continue;
            if (g_client < 0 && !g_stop) {
                perror("virtual display accept");
                break;
            }
            if (g_client < 0) break;
            CGDisplayStreamFrameAvailableHandler handler = ^(CGDisplayStreamFrameStatus status,
                                                              uint64_t display_time,
                                                              IOSurfaceRef surface,
                                                              CGDisplayStreamUpdateRef update) {
                stream_frame(status, display_time, surface, update);
            };
            CGDisplayStreamRef stream = CGDisplayStreamCreateWithDispatchQueue(
                display.displayID, 160, 128, 'BGRA', NULL, g_stream_queue, handler);
            if (stream == NULL || CGDisplayStreamStart(stream) != kCGErrorSuccess) {
                fprintf(stderr, "failed to start CGDisplayStream\n");
                if (stream != NULL) CFRelease(stream);
                close(g_client);
                g_client = -1;
            } else {
                while (!g_stop && g_client >= 0) {
                    usleep(100000);
                }
                CGDisplayStreamStop(stream);
                CFRelease(stream);
            }
        }
        close(server);
        if (g_client >= 0) close(g_client);
        unlink(socket_path);
        return 0;
    }
}
